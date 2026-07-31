//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Bukti end-to-end enkripsi field transparan (PR-3.8.3/3.8.4) di atas Postgres NYATA.
//
// Unit test di field_crypto_test.go bekerja di atas koneksi palsu: ia membuktikan SQL yang
// DISUSUN repo konsisten dengan dirinya sendiri, bukan bahwa SQL itu cocok dengan tabel yang
// DIHASILKAN generator DDL (PR-3.8.1). Justru di sambungan dua sisi itulah kesalahan paling
// mahal bersembunyi — kolom yang tak cocok, blind index yang tak pernah ter-UNIQUE, atau
// pengenal yang diam-diam mendarat plaintext. Karena itu tabel di sini TIDAK ditulis tangan:
// ia dibangun dari EntityDef lewat db.GenerateMigration, persis seperti modul produksi.

const (
	itTenant = "pemkot-surabaya"
	itNIK    = "3578010101010001"
	itRek    = "1234567890"
)

type pegawaiIT struct {
	ID         uuid.UUID
	Nama       string
	NIK        string
	NoRekening string
	Version    int
}

type pegawaiITMapper struct{}

func (pegawaiITMapper) Table() string           { return "test_crypto.pegawais" }
func (pegawaiITMapper) DataColumns() []string   { return []string{"nama", "nik", "no_rekening"} }
func (pegawaiITMapper) SearchColumns() []string { return []string{"nama"} }
func (pegawaiITMapper) DataValues(e *pegawaiIT) []any {
	return []any{e.Nama, e.NIK, e.NoRekening}
}
func (pegawaiITMapper) Scan(s db.RowScanner) (*pegawaiIT, error) {
	var p pegawaiIT
	if err := s.Scan(&p.ID, &p.Nama, &p.NIK, &p.NoRekening, &p.Version); err != nil {
		return nil, err
	}
	return &p, nil
}
func (pegawaiITMapper) ID(e *pegawaiIT) uuid.UUID      { return e.ID }
func (pegawaiITMapper) Version(e *pegawaiIT) int       { return e.Version }
func (pegawaiITMapper) SetVersion(e *pegawaiIT, v int) { e.Version = v }

// pegawaiITDef: nama ber-class personal (TIDAK dienkripsi — harus tetap bisa dicari),
// nik & no_rekening ber-class personal_id (dienkripsi). nik Unique lewat blind index;
// no_rekening searchable tanpa Unique. Kombinasi ini yang menurunkan seluruh DDL di bawah.
func pegawaiITDef() domain.EntityDef {
	return domain.EntityDef{
		Name: "Pegawai", Schema: "test_crypto", Tier: domain.Tier1,
		Audit: domain.Audited{}, Lockable: domain.NotLockable{},
		Fields: []domain.FieldDef{
			{Name: "nama", Type: domain.FieldText, Class: domain.DataPersonal, Required: true},
			{Name: "nik", Type: domain.FieldText, Class: domain.DataPersonalID, Searchable: true, Unique: true},
			{Name: "no_rekening", Type: domain.FieldText, Class: domain.DataPersonalID, Searchable: true},
		},
		Searchable: []string{"nama"},
	}
}

type cryptoFixture struct {
	repo      port.BaseRepository[pegawaiIT]
	pool      *db.Pool
	svc       *crypto.Service
	auditRepo *db.AuditRepo
	ctx       *testkit.TestContext // AuthContext + port.WithTenant, seperti gateway.Context
	actor     uuid.UUID
}

// setupCryptoRepo menyiapkan satu Postgres berisi: schema id (kunci & registry tenant),
// schema gov (audit), dan schema test_crypto (tabel entity dari generator DDL).
func setupCryptoRepo(t *testing.T) *cryptoFixture {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()

	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	reset := `DROP SCHEMA IF EXISTS test_crypto CASCADE;
		DROP SCHEMA IF EXISTS gov CASCADE;
		DROP SCHEMA IF EXISTS id CASCADE`
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), reset)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, reset); err != nil {
		t.Fatalf("reset schema: %v", err)
	}

	// Identity DB: di sinilah DEK ter-wrap & custody tenant hidup (ADR-010 §2).
	for _, name := range []string{
		"001_create_identity.up.sql",
		"002_create_tenant_registry.up.sql",
		"007_create_data_keys.up.sql",
		"008_add_key_custody_tenant_registry.up.sql",
	} {
		sql, err := os.ReadFile(filepath.Join("..", "..", "identity", "migrations", name))
		if err != nil {
			t.Fatalf("baca migrasi %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migrasi %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO id.tenant_registry
		(tenant_id, nama, tier, db_host, db_name, key_custody)
		VALUES ($1, $1, 1, 'db', 'gov_test', 'platform')`, itTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	svc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production", // driver static = jalur produksi Tier 1/2, bukan jalur dev
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, pool)
	if err != nil {
		t.Fatalf("crypto.NewFromConfig: %v", err)
	}

	// Tabel dibangun dari EntityDef — bukan DDL tulis tangan. Inilah yang membuat test ini
	// menguji SAMBUNGAN generator DDL ↔ repo, bukan sekadar repo.
	def := pegawaiITDef()
	if err := def.Validate(); err != nil {
		t.Fatalf("EntityDef tidak valid: %v", err)
	}
	up, _, err := db.GenerateMigration("test_crypto", []domain.EntityDef{def})
	if err != nil {
		t.Fatalf("generate migration: %v", err)
	}
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply DDL entity:\n%s\nerror: %v", up, err)
	}

	auditRepo := db.NewAuditRepo(pool)
	if err := auditRepo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit: %v", err)
	}

	repo, err := db.NewRepository[pegawaiIT](pool, pegawaiITMapper{}, def, audit.NewEngine(auditRepo), db.WithCrypto(svc))
	if err != nil {
		t.Fatalf("factory repo: %v", err)
	}

	actor := uuid.New()
	return &cryptoFixture{
		repo: repo, pool: pool, svc: svc, auditRepo: auditRepo, actor: actor,
		ctx: testkit.Ctx(t, testkit.WithTenant(itTenant), testkit.WithPersonID(actor)),
	}
}

func (f *cryptoFixture) save(t *testing.T, nama, nik, rek string) *pegawaiIT {
	t.Helper()
	p := &pegawaiIT{ID: uuid.New(), Nama: nama, NIK: nik, NoRekening: rek}
	if err := f.repo.Save(f.ctx, p); err != nil {
		t.Fatalf("save %s: %v", nama, err)
	}
	return p
}

// TestFieldCrypto_KolomFisikSesuaiDDL mengunci bentuk tabel: kolom plaintext untuk field
// terenkripsi TIDAK BOLEH ADA. Bila suatu saat generator kembali membuat kolom `nik`,
// kesalahan itu tak akan terlihat dari perilaku repo (INSERT tetap sukses) — hanya
// pemeriksaan katalog seperti ini yang menangkapnya.
func TestFieldCrypto_KolomFisikSesuaiDDL(t *testing.T) {
	f := setupCryptoRepo(t)
	ctx := context.Background()

	rows, err := f.pool.Query(ctx, `SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'test_crypto' AND table_name = 'pegawais'`)
	if err != nil {
		t.Fatalf("baca katalog: %v", err)
	}
	defer rows.Close()
	cols := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan katalog: %v", err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterasi katalog: %v", err)
	}

	for _, plain := range []string{"nik", "no_rekening"} {
		if _, ada := cols[plain]; ada {
			t.Errorf("kolom plaintext %q ada di tabel — nilai terenkripsi tak boleh punya kolom mentah", plain)
		}
	}
	for _, enc := range []string{"nik_enc", "nik_bidx", "no_rekening_enc", "no_rekening_bidx"} {
		if got := cols[enc]; got != "bytea" {
			t.Errorf("kolom %s bertipe %q, mau bytea", enc, got)
		}
	}
	if cols["nama"] != "text" {
		t.Errorf("kolom nama = %q, mau text (class personal TIDAK dienkripsi agar tetap bisa dicari)", cols["nama"])
	}
}

// TestFieldCrypto_RoundtripCRUD: nilai kembali utuh lewat repo, sementara yang tersimpan di
// tabel adalah ciphertext. Ini DoD inti — "SELECT nik_enc bukan plaintext".
func TestFieldCrypto_RoundtripCRUD(t *testing.T) {
	f := setupCryptoRepo(t)
	p := f.save(t, "Budi", itNIK, itRek)

	got, err := f.repo.FindByID(f.ctx, p.ID)
	if err != nil {
		t.Fatalf("findByID: %v", err)
	}
	if got.NIK != itNIK || got.NoRekening != itRek || got.Nama != "Budi" {
		t.Fatalf("hasil dekripsi tidak cocok: %+v", got)
	}

	var nikEnc, nikBidx []byte
	var nama string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT nik_enc, nik_bidx, nama FROM test_crypto.pegawais WHERE id = $1`, p.ID,
	).Scan(&nikEnc, &nikBidx, &nama); err != nil {
		t.Fatalf("baca kolom mentah: %v", err)
	}
	if bytes.Contains(nikEnc, []byte(itNIK)) {
		t.Fatal("nik_enc memuat NIK plaintext")
	}
	if bytes.Contains(nikBidx, []byte(itNIK)) {
		t.Fatal("nik_bidx memuat NIK plaintext")
	}
	if len(nikBidx) == 0 {
		t.Fatal("nik_bidx kosong — equality lookup & UNIQUE tak akan bekerja")
	}
	if nama != "Budi" {
		t.Fatalf("nama tersimpan %q — class personal seharusnya plaintext", nama)
	}

	// Update: nilai baru ikut terenkripsi, versi naik, blind index ikut berpindah.
	got.NoRekening = "9998887776"
	if err := f.repo.Update(f.ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := f.repo.FindByID(f.ctx, p.ID)
	if err != nil {
		t.Fatalf("findByID setelah update: %v", err)
	}
	if after.NoRekening != "9998887776" || after.Version != 2 {
		t.Fatalf("hasil update: %+v", after)
	}

	if err := f.repo.SoftDelete(f.ctx, p.ID); err != nil {
		t.Fatalf("softDelete: %v", err)
	}
	if _, err := f.repo.FindByID(f.ctx, p.ID); err == nil {
		t.Fatal("findByID setelah soft delete harus gagal")
	}
}

// TestFieldCrypto_ListFilterLewatBlindIndex: equality atas kolom terenkripsi dialihkan ke
// _bidx. Bila nama kolom blind index yang disusun repo berbeda dari yang dibuat DDL, query
// ini gagal keras (kolom tak ada) — itulah gunanya dijalankan di DB nyata.
func TestFieldCrypto_ListFilterLewatBlindIndex(t *testing.T) {
	f := setupCryptoRepo(t)
	f.save(t, "Budi", itNIK, itRek)
	f.save(t, "Siti", "3578010101010002", "2222222222")
	f.save(t, "Andi", "3578010101010003", "3333333333")

	res, err := f.repo.List(f.ctx, port.ListFilter{Filters: map[string]any{"nik": itNIK}})
	if err != nil {
		t.Fatalf("list filter nik: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("filter nik total=%d items=%d, mau 1/1", res.Total, len(res.Items))
	}
	if res.Items[0].Nama != "Budi" || res.Items[0].NIK != itNIK {
		t.Fatalf("baris yang ditemukan salah: %+v", res.Items[0])
	}

	kosong, err := f.repo.List(f.ctx, port.ListFilter{Filters: map[string]any{"nik": "3578019999999999"}})
	if err != nil {
		t.Fatalf("list filter nik tak dikenal: %v", err)
	}
	if kosong.Total != 0 {
		t.Fatalf("NIK asing seharusnya tak menemukan apa pun, dapat %d", kosong.Total)
	}

	// Kolom biasa tetap lewat jalur lama, termasuk pencarian ILIKE.
	cari, err := f.repo.List(f.ctx, port.ListFilter{Search: "ud"})
	if err != nil {
		t.Fatalf("list search nama: %v", err)
	}
	if cari.Total != 1 || cari.Items[0].Nama != "Budi" {
		t.Fatalf("pencarian nama gagal: total=%d %+v", cari.Total, cari.Items)
	}
}

// TestFieldCrypto_UniqueDitegakkanBlindIndex: UNIQUE tak mungkin di _enc (nonce acak), jadi
// ia menempel di _bidx. Test ini membuktikan constraint itu benar-benar ada dan menggigit —
// duplikat NIK harus ditolak DB meski ciphertext-nya berbeda.
func TestFieldCrypto_UniqueDitegakkanBlindIndex(t *testing.T) {
	f := setupCryptoRepo(t)
	f.save(t, "Budi", itNIK, itRek)

	kembar := &pegawaiIT{ID: uuid.New(), Nama: "Budi Kembar", NIK: itNIK, NoRekening: "5555555555"}
	err := f.repo.Save(f.ctx, kembar)
	if err == nil {
		t.Fatal("NIK duplikat harus ditolak UNIQUE pada nik_bidx")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("error harus unique_violation (23505), dapat: %v", err)
	}
	if !strings.Contains(pgErr.ConstraintName, "nik_bidx") {
		t.Fatalf("constraint yang menolak = %q, mau yang menempel di nik_bidx", pgErr.ConstraintName)
	}

	// no_rekening searchable TANPA Unique: duplikat harus tetap diterima.
	if err := f.repo.Save(f.ctx, &pegawaiIT{
		ID: uuid.New(), Nama: "Rekening Bersama", NIK: "3578010101010009", NoRekening: itRek,
	}); err != nil {
		t.Fatalf("no_rekening duplikat seharusnya boleh (tanpa Unique): %v", err)
	}
}

// TestFieldCrypto_CiphertextDipindahAntarKolomDitolak menutup celah yang TIDAK bisa ditutup
// AAD: ciphertext membawa purpose-nya sendiri dan AAD hanya mengikat tenant, sehingga blob
// yang disalin antar kolom dalam SATU tenant tetap bisa didekripsi. Pemeriksaan PurposeOf di
// lapis repo-lah penegaknya — di sini dibuktikan dengan memindahkan blob lewat SQL langsung.
func TestFieldCrypto_CiphertextDipindahAntarKolomDitolak(t *testing.T) {
	f := setupCryptoRepo(t)
	p := f.save(t, "Budi", itNIK, itRek)

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE test_crypto.pegawais SET no_rekening_enc = nik_enc WHERE id = $1`, p.ID); err != nil {
		t.Fatalf("pindahkan ciphertext: %v", err)
	}

	_, err := f.repo.FindByID(f.ctx, p.ID)
	if err == nil {
		t.Fatal("ciphertext NIK yang dipindah ke kolom no_rekening harus ditolak")
	}
	if !strings.Contains(err.Error(), "purpose") {
		t.Fatalf("error harus menyebut ketidakcocokan purpose, dapat: %v", err)
	}
}

// TestFieldCrypto_AuditDiffTanpaPlaintext adalah DoD PR-3.8.4: mengenkripsi kolom tanpa
// menutup diff hanya MEMINDAHKAN kebocoran ke gov.audit_logs — snapshot audit diambil dari
// entity (plaintext), bukan dari kolom DB. Dump tabel audit di sini harus bersih dari NIK,
// nilainya tetap dapat dipulihkan dengan kunci (bukti tetap bukti), dan hash chain utuh.
func TestFieldCrypto_AuditDiffTanpaPlaintext(t *testing.T) {
	f := setupCryptoRepo(t)
	ctx := context.Background()

	p := f.save(t, "Budi", itNIK, itRek)
	const nikBaru = "3578010101019999"
	p.NIK = nikBaru
	if err := f.repo.Update(f.ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Dump apa adanya: seluruh isi tabel audit sebagai teks.
	var dump string
	if err := f.pool.QueryRow(ctx,
		`SELECT coalesce(string_agg(diff::text, ' '), '') FROM gov.audit_logs`).Scan(&dump); err != nil {
		t.Fatalf("dump diff: %v", err)
	}
	for _, bocor := range []string{itNIK, nikBaru, itRek} {
		if strings.Contains(dump, bocor) {
			t.Fatalf("gov.audit_logs.diff memuat pengenal plaintext %q", bocor)
		}
	}
	// Nama (class personal) TIDAK dienkripsi — diff-nya memang harus terbaca.
	if !strings.Contains(dump, "Budi") {
		t.Fatal("diff kehilangan nilai non-sensitif (nama) — enkripsi terlalu lebar")
	}

	entries, err := f.auditRepo.ByEntity(ctx, "test_crypto.Pegawai", p.ID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry audit = %d, mau 2 (create + update)", len(entries))
	}

	// Bukti tetap bisa diperiksa: nilai NIK di diff adalah ciphertext ber-purpose "nik"
	// yang mendekripsi kembali ke nilai sebelum & sesudah.
	var nikDiff *audit.FieldDiff
	for i, d := range entries[1].Diff {
		if d.Field == "nik" {
			nikDiff = &entries[1].Diff[i]
		}
	}
	if nikDiff == nil {
		t.Fatalf("update tidak mencatat diff nik: %+v", entries[1].Diff)
	}
	before := decryptDiffValue(t, f.svc, nikDiff.Before)
	after := decryptDiffValue(t, f.svc, nikDiff.After)
	if before != itNIK || after != nikBaru {
		t.Fatalf("diff nik terdekripsi = %q -> %q, mau %q -> %q", before, after, itNIK, nikBaru)
	}

	// Hash chain tetap utuh: enkripsi diff terjadi SEBELUM hash dihitung, jadi verifikasi
	// integritas tidak terpengaruh.
	all, err := f.auditRepo.ByTenant(ctx, itTenant)
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	if res := audit.VerifyChain(all); !res.OK {
		t.Fatalf("hash chain putus di entry %d: %s", res.BrokenAt, res.Reason)
	}
}

// TestFieldCrypto_AuditDiffHanyaFieldYangBerubah menjaga janji dasar audit: diff memuat
// HANYA field yang benar-benar berubah. Nonce acak membuat janji itu mudah runtuh tanpa
// gejala — tiap sisi yang dienkripsi sendiri menghasilkan blob berbeda untuk nilai yang
// sama, sehingga setiap update seolah mengubah NIK. Diperiksa lewat tabel nyata karena
// inilah yang akan dibaca pemeriksa.
func TestFieldCrypto_AuditDiffHanyaFieldYangBerubah(t *testing.T) {
	f := setupCryptoRepo(t)
	ctx := context.Background()

	p := f.save(t, "Budi", itNIK, itRek)
	p.Nama = "Budi Santoso" // hanya kolom non-sensitif
	if err := f.repo.Update(f.ctx, p); err != nil {
		t.Fatalf("update nama: %v", err)
	}

	entries, err := f.auditRepo.ByEntity(ctx, "test_crypto.Pegawai", p.ID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry audit = %d, mau 2 (create + update)", len(entries))
	}
	upd := entries[1].Diff
	if len(upd) != 1 || upd[0].Field != "nama" {
		t.Fatalf("update nama harus mencatat tepat 1 field (nama), dapat %+v", upd)
	}

	// Update tanpa perubahan apa pun tidak boleh melahirkan entry (supresi no-op di
	// audit.Engine bergantung pada diff yang kosong).
	if err := f.repo.Update(f.ctx, p); err != nil {
		t.Fatalf("update no-op: %v", err)
	}
	after, err := f.auditRepo.ByEntity(ctx, "test_crypto.Pegawai", p.ID)
	if err != nil {
		t.Fatalf("byEntity setelah no-op: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("update tanpa perubahan menambah entry audit: %d entry", len(after))
	}
}

// TestFieldCrypto_AuditReadGating menutup sisi BACA dari DoD 3.8.4: nilai yang sudah
// terenkripsi di DB hanya terbuka bagi pemegang `audit:sensitive:baca`. Diuji ujung ke ujung
// (repo menulis → tabel nyata → Reader membaca) karena inilah rantai yang benar-benar dilalui
// pemeriksa; unit test Reader memakai entry buatan, bukan yang lahir dari mutasi.
func TestFieldCrypto_AuditReadGating(t *testing.T) {
	f := setupCryptoRepo(t)
	p := f.save(t, "Budi", itNIK, itRek)

	reader := audit.NewReader(f.auditRepo, f.svc)

	tanpaIzin, err := reader.ByEntity(f.ctx, "test_crypto.Pegawai", p.ID)
	if err != nil {
		t.Fatalf("baca tanpa izin: %v", err)
	}
	if len(tanpaIzin) != 1 {
		t.Fatalf("entry = %d, mau 1 — entry tak boleh ikut disembunyikan", len(tanpaIzin))
	}
	var nikTertutup, namaTerbaca bool
	for _, d := range tanpaIzin[0].Diff {
		switch d.Field {
		case "nik":
			nikTertutup = d.After == audit.HiddenSensitive
		case "nama":
			namaTerbaca = d.After == "Budi"
		}
	}
	if !nikTertutup {
		t.Fatalf("nik harus tertutup tanpa %s: %+v", audit.PermSensitiveBaca, tanpaIzin[0].Diff)
	}
	if !namaTerbaca {
		t.Fatalf("nilai non-sensitif ikut tertutup: %+v", tanpaIzin[0].Diff)
	}

	berizin := testkit.Ctx(t, testkit.WithTenant(itTenant), testkit.WithPersonID(f.actor),
		testkit.WithPermission(audit.PermSensitiveBaca))
	dibuka, err := reader.ByEntity(berizin, "test_crypto.Pegawai", p.ID)
	if err != nil {
		t.Fatalf("baca dengan izin: %v", err)
	}
	var nikTerbuka bool
	for _, d := range dibuka[0].Diff {
		if d.Field == "nik" {
			nikTerbuka = d.After == itNIK
		}
	}
	if !nikTerbuka {
		t.Fatalf("pemegang %s harus melihat NIK: %+v", audit.PermSensitiveBaca, dibuka[0].Diff)
	}
}

// decryptDiffValue membuka satu nilai diff terenkripsi (base64 ciphertext) — mewakili apa
// yang kelak dilakukan pembaca audit ber-permission `audit:sensitive:baca`.
func decryptDiffValue(t *testing.T, svc *crypto.Service, v any) string {
	t.Helper()
	// Diff kembali dari JSONB, jadi nilainya string (atau json.Number untuk angka).
	s, ok := v.(string)
	if !ok {
		b, _ := json.Marshal(v)
		t.Fatalf("nilai diff bukan string base64: %s", b)
	}
	ct, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("nilai diff %q bukan base64 — pengenal mungkin tersimpan mentah: %v", s, err)
	}
	purpose, err := svc.PurposeOf(ct)
	if err != nil {
		t.Fatalf("PurposeOf: %v", err)
	}
	if purpose != "nik" {
		t.Fatalf("purpose ciphertext diff = %q, mau nik", purpose)
	}
	plain, err := svc.Decrypt(context.Background(), itTenant, ct)
	if err != nil {
		t.Fatalf("dekripsi nilai diff: %v", err)
	}
	return string(plain)
}
