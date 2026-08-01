package crypto

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
)

// stubRow & stubConn meniru port.DBConn seminimal mungkin: hanya QueryRow yang dipakai
// resolver custody. Query lain sengaja panic — bila kelak resolver menyentuh jalur lain,
// test harus gagal keras, bukan diam.
type stubRow struct {
	value string
	err   error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.value
	return nil
}

type stubConn struct {
	row    stubRow
	querie int
}

func (c *stubConn) QueryRow(context.Context, string, ...any) port.Row {
	c.querie++
	return c.row
}

func (c *stubConn) Query(context.Context, string, ...any) (port.Rows, error) {
	panic("tak dipakai")
}

func (c *stubConn) Exec(context.Context, string, ...any) (port.CommandTag, error) {
	panic("tak dipakai")
}

func TestDBCustodyResolver_BacaDanCache(t *testing.T) {
	conn := &stubConn{row: stubRow{value: string(CustodyPlatform)}}
	r := NewDBCustodyResolver(conn, time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		got, err := r.Custody(ctx, "pemkot-surabaya")
		if err != nil {
			t.Fatalf("Custody #%d: %v", i, err)
		}
		if got != CustodyPlatform {
			t.Fatalf("custody = %q, mau %q", got, CustodyPlatform)
		}
	}
	if conn.querie != 1 {
		t.Fatalf("query ke registry = %d, mau 1 (sisanya dari cache)", conn.querie)
	}

	// Setelah TTL lewat, perubahan kebijakan terlihat tanpa restart.
	conn.row = stubRow{value: string(CustodyTenant)}
	r.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	got, err := r.Custody(ctx, "pemkot-surabaya")
	if err != nil {
		t.Fatalf("Custody setelah TTL: %v", err)
	}
	if got != CustodyTenant {
		t.Fatalf("custody setelah TTL = %q, mau %q", got, CustodyTenant)
	}
}

func TestDBCustodyResolver_TenantTakTerdaftarFailClosed(t *testing.T) {
	r := NewDBCustodyResolver(&stubConn{row: stubRow{err: pgx.ErrNoRows}}, time.Minute)
	if _, err := r.Custody(context.Background(), "tenant-hantu"); err == nil {
		t.Fatal("tenant yang tak ada di registry harus gagal (fail-closed), bukan dapat kunci")
	}
}

func TestDBCustodyResolver_ErrorDBDiteruskan(t *testing.T) {
	r := NewDBCustodyResolver(&stubConn{row: stubRow{err: errors.New("koneksi putus")}}, time.Minute)
	if _, err := r.Custody(context.Background(), "pemkot-surabaya"); err == nil {
		t.Fatal("error DB harus diteruskan, tidak boleh diam-diam dianggap platform")
	}
}

// TestCustodyTenant_DitolakLantang mengunci keputusan PR-3.8.2: mode custody `tenant` belum
// punya KeyProvider. Ia HARUS gagal, bukan diam-diam dilayani kunci platform — pemda yang
// memilih memegang kuncinya sendiri tak boleh diberi jaminan yang tidak benar.
func TestCustodyTenant_DitolakLantang(t *testing.T) {
	provider, err := newLocalProvider(config.CryptoConfig{})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	svc, err := New(newMemDEKStore(), FixedCustody(CustodyTenant), 0,
		CustodyProvider{Custody: CustodyPlatform, Driver: DriverLocal, Provider: provider})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	_, encErr := svc.Encrypt(ctx, fref("pemda-berdaulat", "nik"), []byte("x"))
	_, bidxErr := svc.BlindIndex(ctx, "pemda-berdaulat", "nik", []byte("x"))
	for name, err := range map[string]error{"Encrypt": encErr, "BlindIndex": bidxErr} {
		if !errors.Is(err, ErrCustodyUnsupported) {
			t.Errorf("%s: err = %v, mau ErrCustodyUnsupported", name, err)
		}
	}
}

// TestCustodyBerpindah_DataLamaTetapTerbaca menguji seam yang membuat penambahan custody kelak
// tidak merusak data: DEK dibuka dengan provider yang MEMBUNGKUSNYA (kolom custody di baris),
// bukan dengan custody tenant saat ini.
func TestCustodyBerpindah_DataLamaTetapTerbaca(t *testing.T) {
	platform, err := newLocalProvider(config.CryptoConfig{})
	if err != nil {
		t.Fatalf("provider platform: %v", err)
	}
	// Provider "pemda" dibuat berbeda (master key berbeda) agar tertukar pasti gagal.
	tenantSide, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x77)})
	if err != nil {
		t.Fatalf("provider tenant: %v", err)
	}

	store := newMemDEKStore()
	custody := &mutableCustody{current: CustodyPlatform}
	svc, err := New(store, custody, 0,
		CustodyProvider{Custody: CustodyPlatform, Driver: DriverLocal, Provider: platform},
		CustodyProvider{Custody: CustodyTenant, Driver: DriverStatic, Provider: tenantSide},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	const tenant = "pemkot-surabaya"

	ctLama, err := svc.Encrypt(ctx, fref(tenant, "nik"), []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt custody platform: %v", err)
	}

	// Custody berpindah ke pemda; cache dikadaluarsakan agar resolusi diulang.
	custody.current = CustodyTenant
	svc.keys.now = func() time.Time { return time.Now().Add(time.Hour) }

	plain, err := svc.Decrypt(ctx, rref(tenant), ctLama)
	if err != nil {
		t.Fatalf("Decrypt data lama setelah custody berpindah: %v", err)
	}
	if string(plain) != "3578010101010001" {
		t.Fatalf("plaintext = %q", plain)
	}
}

type mutableCustody struct{ current Custody }

func (m *mutableCustody) Custody(context.Context, string) (Custody, error) { return m.current, nil }

func TestNewFromConfig(t *testing.T) {
	conn := &stubConn{row: stubRow{value: string(CustodyPlatform)}}

	t.Run("local di development", func(t *testing.T) {
		svc, err := NewFromConfig(&config.AppConfig{Env: "development",
			Crypto: config.CryptoConfig{KMSDriver: DriverLocal}}, conn)
		if err != nil {
			t.Fatalf("err tak terduga: %v", err)
		}
		if svc == nil {
			t.Fatal("service nil")
		}
	})

	// Driver kosong = local (dev tanpa konfigurasi apa pun) — dan karena itu ikut ditolak di
	// luar development oleh subtest berikutnya.
	t.Run("driver kosong dianggap local", func(t *testing.T) {
		if _, err := NewFromConfig(&config.AppConfig{Env: "development"}, conn); err != nil {
			t.Fatalf("err tak terduga: %v", err)
		}
	})

	t.Run("local ditolak di luar development", func(t *testing.T) {
		for _, env := range []string{"staging", "production"} {
			for _, driver := range []string{DriverLocal, ""} {
				_, err := NewFromConfig(&config.AppConfig{Env: env,
					Crypto: config.CryptoConfig{KMSDriver: driver}}, conn)
				if err == nil {
					t.Errorf("env=%s driver=%q: harus ditolak (kunci dev ada di source code)", env, driver)
				}
			}
		}
	})

	t.Run("static butuh master key", func(t *testing.T) {
		_, err := NewFromConfig(&config.AppConfig{Env: "production",
			Crypto: config.CryptoConfig{KMSDriver: DriverStatic}}, conn)
		if !errors.Is(err, ErrMasterKeyRequired) {
			t.Fatalf("err = %v, mau ErrMasterKeyRequired", err)
		}
	})

	t.Run("static dengan master key", func(t *testing.T) {
		svc, err := NewFromConfig(&config.AppConfig{Env: "production",
			Crypto: config.CryptoConfig{KMSDriver: DriverStatic, MasterKey: masterKey(0x88)}}, conn)
		if err != nil {
			t.Fatalf("err tak terduga: %v", err)
		}
		if svc == nil {
			t.Fatal("service nil")
		}
	})

	t.Run("argumen wajib", func(t *testing.T) {
		if _, err := NewFromConfig(nil, conn); err == nil {
			t.Error("config nil harus gagal")
		}
		if _, err := NewFromConfig(&config.AppConfig{Env: "development"}, nil); err == nil {
			t.Error("koneksi identity DB nil harus gagal")
		}
	})
}

// TestDBDEKStore_QueryKeIdentityDB memastikan store menyusun query ke tabel yang benar —
// id.data_keys di identity DB, bukan tenant DB (ADR-010 §2). Perilaku DB nyata diuji di
// integration test.
func TestDBDEKStore_QueryKeIdentityDB(t *testing.T) {
	rec := &recordingConn{row: stubRow{err: pgx.ErrNoRows}}
	store := NewDBDEKStore(rec)
	ref := testRef()

	if _, found, err := store.Active(context.Background(), ref); err != nil || found {
		t.Fatalf("Active: found=%v err=%v, mau found=false err=nil", found, err)
	}
	if len(rec.queries) != 1 {
		t.Fatalf("jumlah query = %d, mau 1", len(rec.queries))
	}
	q := rec.queries[0]
	for _, want := range []string{"id.data_keys", "is_active"} {
		if !strings.Contains(q, want) {
			t.Errorf("query tidak memuat %q: %s", want, q)
		}
	}
}

type recordingConn struct {
	row     stubRow
	queries []string
}

func (c *recordingConn) QueryRow(_ context.Context, sql string, _ ...any) port.Row {
	c.queries = append(c.queries, sql)
	return c.row
}

func (c *recordingConn) Query(context.Context, string, ...any) (port.Rows, error) {
	panic("tak dipakai")
}

func (c *recordingConn) Exec(context.Context, string, ...any) (port.CommandTag, error) {
	panic("tak dipakai")
}
