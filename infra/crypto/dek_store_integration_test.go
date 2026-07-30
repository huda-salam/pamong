//go:build integration

package crypto

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/config"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupIdentityDB menyiapkan schema id + migrasi yang dibutuhkan store DEK (001 untuk schema
// id, 002+008 untuk tenant_registry ber-key_custody, 007 untuk data_keys).
func setupIdentityDB(t *testing.T) (*infradb.Pool, context.Context) {
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
	pool := infradb.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id CASCADE`)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS id CASCADE`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}

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
	return pool, ctx
}

func seedTenant(t *testing.T, pool *infradb.Pool, ctx context.Context, tenantID, custody string) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO id.tenant_registry
		(tenant_id, nama, tier, db_host, db_name, key_custody) VALUES ($1, $2, 1, 'db', $3, $4)`,
		tenantID, tenantID, "gov_"+tenantID, custody)
	if err != nil {
		t.Fatalf("seed tenant %s: %v", tenantID, err)
	}
}

func TestDBDEKStore_InsertLaluBaca(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	store := NewDBDEKStore(pool)
	ref := testRef()

	if _, found, err := store.Active(ctx, ref); err != nil || found {
		t.Fatalf("Active sebelum insert: found=%v err=%v", found, err)
	}

	rec := DEKRecord{Version: 1, Wrapped: []byte("wrapped-dek-1"), Custody: CustodyPlatform, KEKDriver: DriverStatic}
	saved, err := store.InsertActive(ctx, ref, rec)
	if err != nil {
		t.Fatalf("InsertActive: %v", err)
	}
	if saved.Version != 1 || !bytes.Equal(saved.Wrapped, rec.Wrapped) || saved.KEKDriver != DriverStatic {
		t.Fatalf("baris tersimpan = %+v", saved)
	}

	got, found, err := store.Active(ctx, ref)
	if err != nil || !found {
		t.Fatalf("Active: found=%v err=%v", found, err)
	}
	if !bytes.Equal(got.Wrapped, rec.Wrapped) || got.Custody != CustodyPlatform {
		t.Fatalf("Active = %+v", got)
	}

	byVer, found, err := store.ByVersion(ctx, ref, 1)
	if err != nil || !found {
		t.Fatalf("ByVersion(1): found=%v err=%v", found, err)
	}
	if !bytes.Equal(byVer.Wrapped, rec.Wrapped) {
		t.Fatal("ByVersion mengembalikan blob berbeda")
	}
	if _, found, err := store.ByVersion(ctx, ref, 2); err != nil || found {
		t.Fatalf("ByVersion(2) belum ada: found=%v err=%v", found, err)
	}
}

// TestDBDEKStore_KunciTerpisahPerRef memastikan granularitas (tenant, purpose, kind) benar
// ditegakkan DB — bukan sekadar diasumsikan kode.
func TestDBDEKStore_KunciTerpisahPerRef(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	store := NewDBDEKStore(pool)

	base := testRef()
	variants := map[string]KeyRef{
		"tenant lain":  func() KeyRef { r := base; r.TenantID = "pemkot-malang"; return r }(),
		"purpose lain": func() KeyRef { r := base; r.Purpose = "no_rekening"; return r }(),
		"kind lain":    func() KeyRef { r := base; r.Kind = KindBlindIndex; return r }(),
	}

	if _, err := store.InsertActive(ctx, base, DEKRecord{
		Version: 1, Wrapped: []byte("dek-base"), Custody: CustodyPlatform, KEKDriver: DriverStatic}); err != nil {
		t.Fatalf("insert base: %v", err)
	}
	for name, ref := range variants {
		if _, found, err := store.Active(ctx, ref); err != nil || found {
			t.Errorf("%s: seharusnya belum punya kunci (found=%v err=%v)", name, found, err)
		}
		if _, err := store.InsertActive(ctx, ref, DEKRecord{
			Version: 1, Wrapped: []byte("dek-" + name), Custody: CustodyPlatform, KEKDriver: DriverStatic}); err != nil {
			t.Errorf("%s: insert: %v", name, err)
		}
	}
}

// TestDBDEKStore_BalapanInsertMenghasilkanSatuKunci menguji invariant terpenting store: dua
// proses yang sama-sama menemukan "belum ada kunci" tidak boleh berakhir dengan dua kunci
// aktif (data ditulis dengan kunci berbeda = sebagian tak terbaca). Unique index parsial
// uq_data_keys_active yang menegakkannya; InsertActive mengembalikan baris pemenang.
func TestDBDEKStore_BalapanInsertMenghasilkanSatuKunci(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	store := NewDBDEKStore(pool)
	ref := testRef()

	const goroutines = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results [][]byte
		errs    []error
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := DEKRecord{
				Version: 1,
				Wrapped: []byte{byte('a' + i)}, // blob berbeda per goroutine
				Custody: CustodyPlatform, KEKDriver: DriverStatic,
			}
			saved, err := store.InsertActive(ctx, ref, rec)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results = append(results, saved.Wrapped)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		t.Errorf("InsertActive: %v", err)
	}
	if len(results) != goroutines {
		t.Fatalf("hasil = %d, mau %d", len(results), goroutines)
	}
	// Semua pemanggil WAJIB melihat blob yang sama (baris pemenang).
	for i, got := range results {
		if !bytes.Equal(got, results[0]) {
			t.Fatalf("goroutine %d melihat DEK berbeda (%q vs %q) — dua kunci aktif", i, got, results[0])
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM id.data_keys WHERE tenant_id = $1 AND purpose = $2 AND kind = $3`,
		ref.TenantID, ref.Purpose, string(ref.Kind)).Scan(&count); err != nil {
		t.Fatalf("hitung baris: %v", err)
	}
	if count != 1 {
		t.Fatalf("baris data_keys = %d, mau 1", count)
	}
}

// TestService_EndToEndDenganDBStore menjalankan Service lengkap di atas store & registry
// nyata: kunci dibuat otomatis pada pemakaian pertama, custody dibaca dari registry, dan
// kolom _enc yang tersimpan bukan plaintext.
func TestService_EndToEndDenganDBStore(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	seedTenant(t, pool, ctx, "pemkot-surabaya", string(CustodyPlatform))

	svc, err := NewFromConfig(&config.AppConfig{
		Env:    "production", // driver static = jalur produksi Tier 1/2
		Crypto: config.CryptoConfig{KMSDriver: DriverStatic, MasterKey: masterKey(0x99), DEKCacheTTL: time.Minute},
	}, pool)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	const nik = "3578010101010001"
	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte(nik))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ct, []byte(nik)) {
		t.Fatal("ciphertext memuat NIK plaintext")
	}
	plain, err := svc.Decrypt(ctx, "pemkot-surabaya", ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != nik {
		t.Fatalf("plaintext = %q", plain)
	}
	if _, err := svc.BlindIndex(ctx, "pemkot-surabaya", "nik", []byte(nik)); err != nil {
		t.Fatalf("BlindIndex: %v", err)
	}

	// Dua kunci terbentuk otomatis: enc & bidx (terpisah, ADR-009 §2). Tak ada DEK mentah
	// di DB — hanya blob ter-wrap.
	var jumlah int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM id.data_keys WHERE tenant_id = 'pemkot-surabaya' AND purpose = 'nik'`).Scan(&jumlah); err != nil {
		t.Fatalf("hitung kunci: %v", err)
	}
	if jumlah != 2 {
		t.Fatalf("jumlah kunci = %d, mau 2 (enc + bidx)", jumlah)
	}
}

// TestService_CustodyTenantDitolakLewatRegistry menutup jalur end-to-end keputusan PR-3.8.2:
// tenant yang di-set key_custody='tenant' di registry ditolak lantang, bukan diam-diam
// dilayani kunci platform.
func TestService_CustodyTenantDitolakLewatRegistry(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	seedTenant(t, pool, ctx, "pemda-berdaulat", string(CustodyTenant))

	svc, err := NewFromConfig(&config.AppConfig{
		Env:    "production",
		Crypto: config.CryptoConfig{KMSDriver: DriverStatic, MasterKey: masterKey(0xAA)},
	}, pool)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	if _, err := svc.Encrypt(ctx, "pemda-berdaulat", "nik", []byte("x")); err == nil {
		t.Fatal("tenant ber-custody 'tenant' harus ditolak sampai driver pemda didaftarkan")
	}
}

// TestService_TenantTakTerdaftarDitolak: fail-closed. Tenant di luar registry tak boleh
// mendapat kunci apa pun.
func TestService_TenantTakTerdaftarDitolak(t *testing.T) {
	pool, ctx := setupIdentityDB(t)

	svc, err := NewFromConfig(&config.AppConfig{
		Env:    "production",
		Crypto: config.CryptoConfig{KMSDriver: DriverStatic, MasterKey: masterKey(0xBB)},
	}, pool)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	if _, err := svc.Encrypt(ctx, "tenant-hantu", "nik", []byte("x")); err == nil {
		t.Fatal("tenant tak terdaftar harus ditolak")
	}
}

// TestMigrasi007_DownMenghapusStore memeriksa pasangan down migration benar-benar bersih
// (syarat PR: setiap migration punya down).
func TestMigrasi007_DownMenghapusStore(t *testing.T) {
	pool, ctx := setupIdentityDB(t)

	for _, name := range []string{
		"008_add_key_custody_tenant_registry.down.sql",
		"007_create_data_keys.down.sql",
	} {
		sql, err := os.ReadFile(filepath.Join("..", "..", "identity", "migrations", name))
		if err != nil {
			t.Fatalf("baca %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	var ada bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='id' AND table_name='data_keys')`).Scan(&ada); err != nil {
		t.Fatalf("cek tabel: %v", err)
	}
	if ada {
		t.Fatal("id.data_keys masih ada setelah down migration")
	}
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema='id' AND table_name='tenant_registry' AND column_name='key_custody')`).Scan(&ada); err != nil {
		t.Fatalf("cek kolom: %v", err)
	}
	if ada {
		t.Fatal("kolom key_custody masih ada setelah down migration")
	}
}
