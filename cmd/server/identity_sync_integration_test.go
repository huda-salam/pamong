//go:build integration

// identity_sync_integration_test.go membuktikan jalur clone identity END-TO-END lewat
// perakitan PRODUKSI (identitydomain.RegisterEventSchemas + wireIdentitySync), bukan lewat
// rakitan khusus test:
//
//	terbit identity.employment.ditugaskan → NATS nyata → Engine → RepoCloneSource membaca
//	identity DB (realm sentral) → TenantDBWriter menulis gov.user_profiles tenant (realm tenant)
//	→ UserResolver.ResolveByNIK menemukannya lewat blind index.
//
// Inilah yang tak pernah terbukti sebelum PR ini: seluruh mesin PR-3.8.5a/3.8.5b sudah ada
// tapi tak seorang pun merakitnya di server hidup.
//
// Bus-nya NATS nyata (server embedded), bukan driver memory: memory mengirim SINKRON di
// goroutine pemanggil, sehingga ia tak bisa membedakan "subscriber ter-register" dari
// "subscriber ter-register TEPAT WAKTU" — persis kelas cacat yang pernah muncul sebagai test
// flaky (Subscribe tanpa Flush, defect-nats-subscribe-flush).
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	natssrv "github.com/nats-io/nats-server/v2/server"

	"github.com/huda-salam/pamong/core/config"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitydomain "github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/port"
)

// Pengenal fixture. Nilainya dipakai dua arah: sebagai yang DITULIS ke identity DB dan sebagai
// yang DICARI di dump clone — jadi satu konstanta, bukan dua literal yang bisa menyimpang.
const (
	syncTenant = "pemkot-b"
	syncNIK    = "3578010101900007"
	syncNIP    = "199001012015011007"
	syncEmail  = "rina@example.test"
	syncNoHP   = "0812340007"
	syncNama   = "Rina Kartika"
)

func TestIdentitySync_ClonePersonKeTenantDB(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()

	pgcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cp := pgcfg.ConnConfig

	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(rawPool)
	dropSchemas := func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id, gov CASCADE`) }
	dropSchemas()
	t.Cleanup(func() { dropSchemas(); rawPool.Close() })

	// Skema identity dari file migrasi NYATA (id.persons/id.employments ber-kolom _enc/_bidx,
	// id.data_keys). gov.user_profiles TIDAK di-seed: yang diuji justru apakah writer
	// meng-ensure-nya sendiri saat clone pertama.
	applyUpMigrations(t, ctx, pool, repoPath("identity/migrations"))
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, syncTenant, "Pemkot B", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	// --- Rakit dependency persis seperti run() ---
	idCfg := config.IdentityDBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	identityPool, err := db.NewIdentity(ctx, idCfg)
	if err != nil {
		t.Fatalf("identity pool: %v", err)
	}
	t.Cleanup(identityPool.Close)

	sharedCfg := config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	resolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(resolver, sharedCfg, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

	cryptoSvc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production",
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, identityPool)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}

	// Bus NYATA di atas NATS embedded — asinkron, lintas koneksi, dengan schema registry.
	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "nats", URL: startEmbeddedNATSForSync(t)},
		eventbus.NewSchemaRegistry(),
	)
	if err != nil {
		t.Fatalf("event bus NATS: %v", err)
	}

	// === Yang diuji: perakitan PRODUKSI (registrar identity + wiring composition root) ===
	if err := identitydomain.RegisterEventSchemas(bus.Schema()); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	if err := wireIdentitySync(identityPool, connMgr, cryptoSvc, bus); err != nil {
		t.Fatalf("wireIdentitySync: %v", err)
	}

	// Seed person + employment lewat repo identity (pengenal tersimpan terenkripsi realm SENTRAL).
	// Inilah sumber yang akan dibaca-balik CloneSource — payload event tak membawa satu pun
	// pengenal (ADR-018), jadi clone hanya bisa terisi bila jalur baca-balik benar-benar hidup.
	personRepo, err := identitydb.NewPersonRepo(identityPool, cryptoSvc)
	if err != nil {
		t.Fatalf("NewPersonRepo: %v", err)
	}
	employmentRepo, err := identitydb.NewEmploymentRepo(identityPool, cryptoSvc)
	if err != nil {
		t.Fatalf("NewEmploymentRepo: %v", err)
	}
	personID, employmentID := uuid.New(), uuid.New()
	if err := personRepo.Save(ctx, &identitydomain.Person{
		ID: personID, NIK: syncNIK, NamaLengkap: syncNama,
		NoHP: syncNoHP, Email: syncEmail, IsActive: true,
	}); err != nil {
		t.Fatalf("simpan person: %v", err)
	}
	if err := employmentRepo.Save(ctx, &identitydomain.Employment{
		ID: employmentID, PersonID: personID, Status: identitydomain.StatusASN,
		NIP: syncNIP, InstansiAsal: "Pemkot B", IsActive: true, ValidFrom: time.Now(),
	}); err != nil {
		t.Fatalf("simpan employment: %v", err)
	}

	assignmentID := uuid.New()
	if err := bus.Publish(ctx, port.Event{
		Name:     identitydomain.EventEmploymentDitugaskan,
		TenantID: syncTenant,
		Payload: identitydomain.EmploymentDitugaskanPayload{
			AssignmentID: assignmentID, EmploymentID: employmentID, PersonID: personID,
			TenantID: syncTenant,
			// Nama di payload SENGAJA berbeda dari yang tersimpan: clone wajib memakai nilai
			// dari CloneSource, bukan dari payload (satu baris clone tak boleh dua umur).
			NamaLengkap: "Nama Dari Payload", EmploymentStatus: "asn",
		},
	}); err != nil {
		t.Fatalf("publish ditugaskan: %v", err)
	}

	tenantPool, err := connMgr.Tenant(ctx, syncTenant)
	if err != nil {
		t.Fatalf("pool tenant: %v", err)
	}
	waitForClone(t, ctx, tenantPool, personID)

	// --- Bukti 1: baris clone benar, dan pengenalnya TIDAK terbaca dari dump ---
	var dump, gotNama string
	var gotAssignment uuid.UUID
	if err := tenantPool.QueryRow(ctx,
		`SELECT user_profiles::text, nama_lengkap, assignment_id
		   FROM gov.user_profiles WHERE id = $1`, personID,
	).Scan(&dump, &gotNama, &gotAssignment); err != nil {
		t.Fatalf("baca clone: %v", err)
	}
	if gotNama != syncNama {
		t.Fatalf("nama clone = %q, mau %q (nilai dari CloneSource, bukan dari payload)", gotNama, syncNama)
	}
	if gotAssignment != assignmentID {
		t.Fatalf("assignment_id clone = %s, mau %s", gotAssignment, assignmentID)
	}
	lower := strings.ToLower(dump)
	for _, rahasia := range []string{syncNIK, syncNIP, syncEmail, syncNoHP} {
		if strings.Contains(lower, strings.ToLower(rahasia)) {
			t.Fatalf("pengenal %q muncul plaintext di baris clone", rahasia)
		}
		// Kolom _enc bertipe bytea dan dirender Postgres sebagai hex. Tanpa pemeriksaan bentuk
		// hex ini, nilai yang mendarat MENTAH di kolom bytea akan lolos — hijau di test, terbaca
		// oleh siapa pun yang men-decode dump (pelajaran PR-3.8.5a).
		if strings.Contains(lower, hex.EncodeToString([]byte(rahasia))) {
			t.Fatalf("pengenal %q tersimpan mentah di kolom bytea (terbaca sebagai hex)", rahasia)
		}
	}
	// Kontrol negatif: nama SENGAJA plaintext (kelas `personal`). Tanpa ini, pemeriksaan di atas
	// tetap hijau seandainya query membaca baris kosong dan tak membuktikan apa pun.
	if !strings.Contains(lower, strings.ToLower(syncNama)) {
		t.Fatal("nama_lengkap tak ada di dump — test membaca baris yang salah")
	}

	// --- Bukti 2: pembaca produksi menemukannya lewat blind index ---
	// Ini yang mengunci kecocokan realm penulis↔pembaca: realm yang salah tidak melempar error,
	// ia hanya membuat bidx tak pernah cocok sehingga lookup mati tanpa gejala.
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}
	tenantCtx := port.WithTenant(ctx, syncTenant)
	profile, err := userResolver.ResolveByNIK(tenantCtx, syncNIK)
	if err != nil {
		t.Fatalf("ResolveByNIK lewat blind index: %v", err)
	}
	// NIK ikut diperiksa: ia dibuka dari nik_enc, jadi kecocokannya membuktikan bukan sekadar
	// bidx yang cocok tapi juga ciphertext yang benar-benar bisa dibuka pembaca.
	if profile.ID != personID || profile.NIK != syncNIK || profile.NIP != syncNIP || profile.NamaLengkap != syncNama {
		t.Fatalf("profil hasil resolusi salah: id=%s nik=%q nip=%q nama=%q",
			profile.ID, profile.NIK, profile.NIP, profile.NamaLengkap)
	}
	if _, err := userResolver.ResolveByNIP(tenantCtx, syncNIP); err != nil {
		t.Fatalf("ResolveByNIP lewat blind index: %v", err)
	}
}

// TestIdentitySync_SchemaEventBelumTerdaftarMenolakPublish mengunci prasyarat yang tak terlihat:
// registry schema yang kosong membuat SETIAP publish event identity gagal, jadi subscriber
// sebanyak apa pun tak akan menerima apa-apa. Cakupan daftarnya (event apa saja, payload apa)
// diuji di identity/domain/events_test.go — di sini yang diuji akibatnya di sisi bus.
func TestIdentitySync_SchemaEventBelumTerdaftarMenolakPublish(t *testing.T) {
	bus := eventbus.NewMemory() // sengaja TANPA RegisterEventSchemas
	err := bus.Publish(context.Background(), port.Event{
		Name:    identitydomain.EventEmploymentDitugaskan,
		Payload: identitydomain.EmploymentDitugaskanPayload{},
	})
	if err == nil {
		t.Fatal("publish tanpa schema terdaftar harus ditolak — kalau lolos, prasyarat wiring ini palsu")
	}
	if err := identitydomain.RegisterEventSchemas(bus.Schema()); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	if err := bus.Publish(context.Background(), port.Event{
		Name:    identitydomain.EventEmploymentDitugaskan,
		Payload: identitydomain.EmploymentDitugaskanPayload{},
	}); err != nil {
		t.Fatalf("sesudah registrasi, publish harus lolos: %v", err)
	}
}

// waitForClone menunggu baris clone muncul. NATS mengirim ASINKRON, jadi menunggu adalah bagian
// dari kontrak yang diuji — bukan tidur sembarang: batas waktunya pendek dan kegagalannya
// menyebut apa yang tak terjadi.
func waitForClone(t *testing.T, ctx context.Context, pool *db.Pool, personID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var ada bool
		err := pool.QueryRow(ctx,
			`SELECT count(*) > 0 FROM gov.user_profiles WHERE id = $1`, personID).Scan(&ada)
		if err == nil && ada {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clone tak muncul di gov.user_profiles dalam 10 detik (err terakhir: %v) — "+
				"subscriber tak ter-register, atau handler gagal (NATS Core tak me-retry)", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// startEmbeddedNATSForSync menjalankan NATS in-process pada port acak (pola yang sama dengan
// infra/eventbus/nats_integration_test.go).
func startEmbeddedNATSForSync(t *testing.T) string {
	t.Helper()
	srv, err := natssrv.NewServer(&natssrv.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("buat NATS server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server tidak siap dalam 5 detik")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}
