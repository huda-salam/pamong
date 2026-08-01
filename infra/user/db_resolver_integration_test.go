//go:build integration

package user_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/config"
	identsync "github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/db"
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit/cryptokit"
)

type fixedResolver struct{ dbName string }

func (r fixedResolver) Resolve(_ context.Context, tenantID string) (*port.TenantInfo, error) {
	return &port.TenantInfo{TenantID: tenantID, DBName: r.dbName, IsActive: true}, nil
}

const (
	tenantID = "pemkot-a"
	// tenantLain terdaftar penuh (custody platform) DENGAN SENGAJA: kalau ia dibiarkan tak
	// terdaftar, test isolasi realm akan lulus karena resolver custody fail-closed — bukan
	// karena kuncinya memang terpisah.
	tenantLain = "pemkot-b"
)

// newUserEnv merakit resolver DB + sync writer di atas DSN test (satu DB via connMgr). Skema
// gov di-reset; clone gov.user_profiles dibuat oleh writer saat Upsert pertama.
func newUserEnv(t *testing.T) (*infrauser.DBResolver, *identsync.TenantDBWriter, context.Context) {
	t.Helper()
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
	shared := config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	connMgr := db.NewTenantConnManager(fixedResolver{dbName: cp.Database}, shared, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	_, _ = pool.Exec(ctx, `DROP SCHEMA IF EXISTS gov CASCADE`)
	cryptokit.Cleanup(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov CASCADE`)
		cryptokit.Cleanup(t, pool)
		pgpool.Close()
	})

	// Kripto NYATA: pengenal pada clone terenkripsi (PR-3.8.5), jadi yang diuji di sini adalah
	// SAMBUNGAN penulis ↔ pembaca lewat kunci yang sama — mock akan meluluskan keduanya bahkan
	// bila realm-nya berbeda.
	svc := cryptokit.NewService(t, pool, tenantID, tenantLain)
	resolver, err := infrauser.NewDBResolver(connMgr, svc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}
	writer, err := identsync.NewTenantDBWriter(connMgr, svc)
	if err != nil {
		t.Fatalf("NewTenantDBWriter: %v", err)
	}

	// Context ber-tenant: resolver membaca tenant dari port.TenantFrom (seperti di runtime,
	// disuntik middleware tenant resolver).
	return resolver, writer, port.WithTenant(ctx, tenantID)
}

func TestDBResolver_ResolveByID_NIP_NIK(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	person := uuid.New()
	clone := identsync.UserProfileClone{
		PersonID: person, AssignmentID: uuid.New(),
		NIK: "3573010101900001", NIP: "199001012020121001",
		NamaLengkap: "Budi Santoso", EmploymentStatus: "asn", IsCrossTenant: false,
	}
	if err := writer.Upsert(ctx, tenantID, clone); err != nil {
		t.Fatalf("seed clone: %v", err)
	}

	byID, err := resolver.ResolveByID(ctx, person)
	if err != nil {
		t.Fatalf("ResolveByID: %v", err)
	}
	if byID.ID != person || byID.NIP != clone.NIP || byID.NIK != clone.NIK ||
		byID.NamaLengkap != clone.NamaLengkap || byID.EmploymentStatus != "asn" {
		t.Fatalf("ResolveByID mismatch: %+v", byID)
	}

	byNIP, err := resolver.ResolveByNIP(ctx, clone.NIP)
	if err != nil {
		t.Fatalf("ResolveByNIP: %v", err)
	}
	if byNIP.ID != person {
		t.Fatalf("ResolveByNIP mengembalikan person salah: %+v", byNIP)
	}

	byNIK, err := resolver.ResolveByNIK(ctx, clone.NIK)
	if err != nil {
		t.Fatalf("ResolveByNIK: %v", err)
	}
	if byNIK.ID != person {
		t.Fatalf("ResolveByNIK mengembalikan person salah: %+v", byNIK)
	}
}

func TestDBResolver_NonASN_NIPKosong(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	person := uuid.New()
	// non-ASN: NIP kosong → disimpan NULL → resolver menormalkan ke "".
	clone := identsync.UserProfileClone{
		PersonID: person, AssignmentID: uuid.New(),
		NIK: "3573010101950002", NIP: "", NamaLengkap: "Siti Aminah",
		EmploymentStatus: "non_asn",
	}
	if err := writer.Upsert(ctx, tenantID, clone); err != nil {
		t.Fatalf("seed clone: %v", err)
	}
	got, err := resolver.ResolveByID(ctx, person)
	if err != nil {
		t.Fatalf("ResolveByID: %v", err)
	}
	if got.NIP != "" || got.EmploymentStatus != "non_asn" {
		t.Fatalf("non-ASN harus NIP kosong; got %+v", got)
	}
}

func TestDBResolver_IsCrossTenant(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	person := uuid.New()
	if err := writer.Upsert(ctx, tenantID, identsync.UserProfileClone{
		PersonID: person, AssignmentID: uuid.New(),
		NIK: "3573010101800003", NIP: "198001012010121002",
		NamaLengkap: "PJ Bupati", EmploymentStatus: "asn", IsCrossTenant: true,
	}); err != nil {
		t.Fatalf("seed clone: %v", err)
	}
	cross, err := resolver.IsCrossTenant(ctx, person)
	if err != nil {
		t.Fatalf("IsCrossTenant: %v", err)
	}
	if !cross {
		t.Fatal("IsCrossTenant harus true untuk penugasan lintas-tenant")
	}
}

func TestDBResolver_NotFound(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	// Pastikan skema clone ada (Upsert satu baris lain) agar not-found = baris tak ada, bukan
	// tabel tak ada.
	if err := writer.Upsert(ctx, tenantID, identsync.UserProfileClone{
		PersonID: uuid.New(), AssignmentID: uuid.New(), NIK: "3573010101700004",
		NamaLengkap: "Lain", EmploymentStatus: "asn", NIP: "197001012000121003",
	}); err != nil {
		t.Fatalf("seed clone: %v", err)
	}
	_, err := resolver.ResolveByID(ctx, uuid.New())
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("ingin NOT_FOUND, dapat %v", err)
	}
}

func TestDBResolver_TenantAbsen_Ditolak(t *testing.T) {
	resolver, _, _ := newUserEnv(t)
	// Context TANPA tenant (tak lewat WithTenant) → resolver menolak eksplisit (bug wiring),
	// bukan panik atau salah-route.
	_, err := resolver.ResolveByID(context.Background(), uuid.New())
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "VALIDATION_ERROR" {
		t.Fatalf("tanpa tenant harus ditolak VALIDATION_ERROR, dapat %v", err)
	}
}

func TestDBResolver_HasCentralRole_Unimplemented(t *testing.T) {
	resolver, _, ctx := newUserEnv(t)
	_, err := resolver.HasCentralRole(ctx, uuid.New(), "super_admin")
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "UNIMPLEMENTED" {
		t.Fatalf("HasCentralRole harus UNIMPLEMENTED (DEFERRED), dapat %v", err)
	}
}

// TestDBResolver_ErrorTakMengutipPengenal menjaga jalur samping ADR-009 §6: pesan
// FrameworkError mengalir ke log DAN body HTTP, jadi menyebut NIK/NIP yang dicari akan
// membocorkannya justru lewat jalur yang paling mudah diambil.
func TestDBResolver_ErrorTakMengutipPengenal(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	if err := writer.Upsert(ctx, tenantID, identsync.UserProfileClone{
		PersonID: uuid.New(), AssignmentID: uuid.New(), NIK: "3573010101700005",
		NamaLengkap: "Lain", EmploymentStatus: "asn", NIP: "197001012000121005",
	}); err != nil {
		t.Fatalf("seed clone: %v", err)
	}

	const nikDicari = "3573019999999999"
	const nipDicari = "199912319999129999"
	if _, err := resolver.ResolveByNIK(ctx, nikDicari); err == nil {
		t.Fatal("ResolveByNIK atas NIK asing harus gagal")
	} else if strings.Contains(err.Error(), nikDicari) {
		t.Fatalf("pesan error mengutip NIK: %v", err)
	}
	if _, err := resolver.ResolveByNIP(ctx, nipDicari); err == nil {
		t.Fatal("ResolveByNIP atas NIP asing harus gagal")
	} else if strings.Contains(err.Error(), nipDicari) {
		t.Fatalf("pesan error mengutip NIP: %v", err)
	}
}

// TestDBResolver_RealmTenantLainTakBisaMembaca menguji akibat yang paling penting dari
// pemilihan realm per-tenant: barisnya boleh saja terbaca (di sini fixedResolver sengaja
// mengarahkan semua tenant ke DB yang sama), tapi kuncinya tidak. Baik lookup maupun
// pembukaan nilai gagal — bukan diam-diam mengembalikan data tenant lain.
func TestDBResolver_RealmTenantLainTakBisaMembaca(t *testing.T) {
	resolver, writer, ctx := newUserEnv(t)
	person := uuid.New()
	const nik = "3573010101900006"
	if err := writer.Upsert(ctx, tenantID, identsync.UserProfileClone{
		PersonID: person, AssignmentID: uuid.New(), NIK: nik,
		NamaLengkap: "Budi Santoso", EmploymentStatus: "non_asn",
	}); err != nil {
		t.Fatalf("seed clone: %v", err)
	}

	lain := port.WithTenant(context.Background(), tenantLain)

	// Lookup: bidx dihitung dengan kunci realm lain → tak pernah cocok.
	if _, err := resolver.ResolveByNIK(lain, nik); err == nil {
		t.Fatal("ResolveByNIK dari realm lain menemukan baris — bidx tak terpisah per tenant")
	}
	// Pembukaan nilai: baris ditemukan lewat id (tak butuh kunci), tapi AAD-nya menolak.
	if _, err := resolver.ResolveByID(lain, person); err == nil {
		t.Fatal("ResolveByID dari realm lain membuka pengenal — ciphertext tak terikat realm")
	}
}
