package crypto

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// memDEKStore adalah DEKStore di memori untuk unit test — menggantikan id.data_keys tanpa DB.
// Menyimpan blob ter-wrap apa adanya sehingga jalur wrap/unwrap tetap diuji sungguhan.
type memDEKStore struct {
	rows      map[string]DEKRecord // key: tenant|purpose|kind|version
	activeVer map[string]int       // key: tenant|purpose|kind
	inserts   int
}

func newMemDEKStore() *memDEKStore {
	return &memDEKStore{rows: map[string]DEKRecord{}, activeVer: map[string]int{}}
}

func refKey(ref KeyRef) string {
	return strings.Join([]string{ref.TenantID, ref.Purpose, string(ref.Kind)}, "|")
}

func verKey(ref KeyRef, version int) string {
	return refKey(ref) + "|" + strconv.Itoa(version)
}

func (s *memDEKStore) Active(_ context.Context, ref KeyRef) (DEKRecord, bool, error) {
	v, ok := s.activeVer[refKey(ref)]
	if !ok {
		return DEKRecord{}, false, nil
	}
	rec, ok := s.rows[verKey(ref, v)]
	return rec, ok, nil
}

func (s *memDEKStore) ByVersion(_ context.Context, ref KeyRef, version int) (DEKRecord, bool, error) {
	rec, ok := s.rows[verKey(ref, version)]
	return rec, ok, nil
}

func (s *memDEKStore) InsertActive(_ context.Context, ref KeyRef, rec DEKRecord) (DEKRecord, error) {
	s.inserts++
	if v, ok := s.activeVer[refKey(ref)]; ok {
		return s.rows[verKey(ref, v)], nil // sudah ada versi aktif: pemanggil kalah balapan
	}
	s.rows[verKey(ref, rec.Version)] = rec
	s.activeVer[refKey(ref)] = rec.Version
	return rec, nil
}

// rotateTo menandai versi baru sebagai aktif — meniru rotasi DEK di store nyata.
func (s *memDEKStore) rotateTo(ref KeyRef, rec DEKRecord) {
	s.rows[verKey(ref, rec.Version)] = rec
	s.activeVer[refKey(ref)] = rec.Version
}

// newTestService merakit Service dengan driver local + store memori (custody platform).
func newTestService(t *testing.T) (*Service, *memDEKStore) {
	t.Helper()
	provider, err := newLocalProvider(config.CryptoConfig{})
	if err != nil {
		t.Fatalf("provider local: %v", err)
	}
	store := newMemDEKStore()
	svc, err := New(store, FixedCustody(CustodyPlatform), time.Minute,
		CustodyProvider{Custody: CustodyPlatform, Driver: DriverLocal, Provider: provider})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, store
}

func TestEncrypt_RoundtripDanCiphertextTidakDeterministik(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	nik := []byte("3578010101010001")

	ct1, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", nik)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ct2, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", nik)
	if err != nil {
		t.Fatalf("Encrypt kedua: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("dua ciphertext atas plaintext sama identik — nonce tidak acak, kesamaan nilai bocor")
	}
	if bytes.Contains(ct1, nik) {
		t.Fatal("ciphertext memuat plaintext")
	}

	for i, ct := range [][]byte{ct1, ct2} {
		plain, err := svc.Decrypt(ctx, "pemkot-surabaya", ct)
		if err != nil {
			t.Fatalf("Decrypt ct%d: %v", i+1, err)
		}
		if !bytes.Equal(plain, nik) {
			t.Fatalf("Decrypt ct%d = %q, mau %q", i+1, plain, nik)
		}
	}
}

func TestDecrypt_TenantLainDitolak(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Tenant lain yang SUDAH punya kunci sendiri: DEK berbeda DAN tenantID ikut ke AAD.
	// Ditolak sebagai ciphertext tak sah — sebab detailnya tak dibocorkan.
	if _, err := svc.Encrypt(ctx, "pemkot-malang", "nik", []byte("3578010101010009")); err != nil {
		t.Fatalf("Encrypt tenant kedua: %v", err)
	}
	if _, err := svc.Decrypt(ctx, "pemkot-malang", ct); !errors.Is(err, port.ErrCiphertextInvalid) {
		t.Fatalf("Decrypt lintas tenant (punya kunci): err = %v, mau ErrCiphertextInvalid", err)
	}

	// Tenant yang belum punya kunci sama sekali: ditolak dengan ErrDEKMissing. SENGAJA dibedakan
	// dari ciphertext tak sah — "kunci tak bisa didapat" adalah sinyal operasional serius (baris
	// id.data_keys hilang = data tak terbaca), tak boleh tersamarkan sebagai blob rusak. Yang
	// penting untuk keamanan: keduanya sama-sama menolak, tak ada plaintext yang keluar.
	if _, err := svc.Decrypt(ctx, "pemkot-blitar", ct); !errors.Is(err, ErrDEKMissing) {
		t.Fatalf("Decrypt tenant tanpa kunci: err = %v, mau ErrDEKMissing", err)
	}
}

func TestDecrypt_CiphertextRusak(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0xFF

	cases := map[string][]byte{
		"kosong":             {},
		"versi format asing": {0x09, 0x03, 'n', 'i', 'k'},
		"purpose kosong":     {ctFormatV1, 0x00, 'x', 'x'},
		"header terpotong":   ct[:6],
		"tag dimodifikasi":   tampered,
	}
	for name, blob := range cases {
		if _, err := svc.Decrypt(ctx, "pemkot-surabaya", blob); !errors.Is(err, port.ErrCiphertextInvalid) {
			t.Errorf("%s: err = %v, mau ErrCiphertextInvalid", name, err)
		}
	}
}

func TestBlindIndex_DeterministikDanTerpisahPerKonteks(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	bidx := func(tenant, purpose, val string) []byte {
		t.Helper()
		out, err := svc.BlindIndex(ctx, tenant, purpose, []byte(val))
		if err != nil {
			t.Fatalf("BlindIndex(%s,%s): %v", tenant, purpose, err)
		}
		return out
	}

	a := bidx("pemkot-surabaya", "nik", "3578010101010001")
	if !bytes.Equal(a, bidx("pemkot-surabaya", "nik", "3578010101010001")) {
		t.Fatal("blind index tidak deterministik — equality & UNIQUE mustahil")
	}
	// Spasi tepi tidak boleh membuat indeks berbeda (duplikat lolos UNIQUE).
	if !bytes.Equal(a, bidx("pemkot-surabaya", "nik", "  3578010101010001 ")) {
		t.Fatal("spasi tepi mengubah blind index")
	}
	if bytes.Equal(a, bidx("pemkot-surabaya", "nik", "3578010101010002")) {
		t.Fatal("nilai berbeda menghasilkan blind index sama")
	}
	if bytes.Equal(a, bidx("pemkot-malang", "nik", "3578010101010001")) {
		t.Fatal("tenant berbeda menghasilkan blind index sama — nilai bisa dikorelasikan lintas tenant")
	}
	if bytes.Equal(a, bidx("pemkot-surabaya", "no_rekening", "3578010101010001")) {
		t.Fatal("purpose berbeda menghasilkan blind index sama — blast radius kunci tidak terbatas")
	}

	// Purpose case-folded: email setara tanpa memandang huruf.
	if !bytes.Equal(
		bidx("pemkot-surabaya", "email", "Budi@Pemkot.go.id"),
		bidx("pemkot-surabaya", "email", "budi@pemkot.go.id"),
	) {
		t.Fatal("email dengan huruf berbeda menghasilkan blind index berbeda")
	}
	// Purpose biasa TIDAK di-case-fold (NIK/NIP memang angka; jangan diam-diam mengubah nilai).
	if bytes.Equal(
		bidx("pemkot-surabaya", "nama_bank", "BRI"),
		bidx("pemkot-surabaya", "nama_bank", "bri"),
	) {
		t.Fatal("purpose non-email ikut di-case-fold")
	}
}

func TestBlindIndex_KunciTerpisahDariKunciEnkripsi(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("x")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := svc.BlindIndex(ctx, "pemkot-surabaya", "nik", []byte("x")); err != nil {
		t.Fatalf("BlindIndex: %v", err)
	}

	encRef := KeyRef{TenantID: "pemkot-surabaya", Purpose: "nik", Kind: KindEncryption, Custody: CustodyPlatform}
	bidxRef := encRef
	bidxRef.Kind = KindBlindIndex

	enc, ok, _ := store.Active(ctx, encRef)
	if !ok {
		t.Fatal("DEK enkripsi tidak dibuat")
	}
	bidx, ok, _ := store.Active(ctx, bidxRef)
	if !ok {
		t.Fatal("DEK blind index tidak dibuat")
	}
	if bytes.Equal(enc.Wrapped, bidx.Wrapped) {
		t.Fatal("DEK enkripsi & blind index sama — rotasi kunci enkripsi akan memaksa reindex")
	}
}

func TestEncrypt_DEKDibuatSekaliLaluDiCache(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("3578010101010001")); err != nil {
			t.Fatalf("Encrypt #%d: %v", i, err)
		}
	}
	if store.inserts != 1 {
		t.Fatalf("insert DEK = %d, mau 1 (kunci dibuat sekali lalu dari cache)", store.inserts)
	}
}

func TestDecrypt_SetelahRotasiDEK_CiphertextLamaTetapTerbaca(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	const tenant = "pemkot-surabaya"

	ctLama, err := svc.Encrypt(ctx, tenant, "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt versi 1: %v", err)
	}

	// Rotasi: DEK versi 2 menjadi aktif; versi 1 tetap ada di store.
	provider, err := newLocalProvider(config.CryptoConfig{})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ref := KeyRef{TenantID: tenant, Purpose: "nik", Kind: KindEncryption, Custody: CustodyPlatform}
	_, wrapped, err := provider.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK v2: %v", err)
	}
	store.rotateTo(ref, DEKRecord{Version: 2, Wrapped: wrapped, Custody: CustodyPlatform, KEKDriver: DriverLocal})
	svc.keys.now = func() time.Time { return time.Now().Add(2 * time.Minute) } // kadaluarsakan cache

	ctBaru, err := svc.Encrypt(ctx, tenant, "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt versi 2: %v", err)
	}
	if _, version, _, _, err := parseCiphertext(ctBaru); err != nil || version != 2 {
		t.Fatalf("ciphertext baru versi = %d (err=%v), mau 2", version, err)
	}

	// Inti rotasi lazy: tulisan lama tetap terbaca dengan kunci versinya sendiri.
	plain, err := svc.Decrypt(ctx, tenant, ctLama)
	if err != nil {
		t.Fatalf("Decrypt ciphertext versi 1 setelah rotasi: %v", err)
	}
	if string(plain) != "3578010101010001" {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestDecrypt_DEKVersiHilang(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Ciphertext menunjuk versi 9 yang tak ada di store (mis. baris data_keys terhapus).
	ct[2+len("nik")+3] = 9
	svc.keys.deks = map[dekCacheKey][]byte{}

	if _, err := svc.Decrypt(ctx, "pemkot-surabaya", ct); !errors.Is(err, ErrDEKMissing) {
		t.Fatalf("err = %v, mau ErrDEKMissing", err)
	}
}

func TestOperasi_ButuhTenantDanPurpose(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Encrypt(ctx, "", "nik", []byte("x")); err == nil {
		t.Error("Encrypt tanpa tenantID harus gagal")
	}
	if _, err := svc.Encrypt(ctx, "pemkot-surabaya", "", []byte("x")); err == nil {
		t.Error("Encrypt tanpa purpose harus gagal")
	}
	if _, err := svc.Encrypt(ctx, "pemkot-surabaya", strings.Repeat("p", purposeMaxLen+1), []byte("x")); err == nil {
		t.Error("purpose melebihi batas header harus gagal (bukan ciphertext rusak saat dibaca)")
	}
	if _, err := svc.BlindIndex(ctx, "", "nik", []byte("x")); err == nil {
		t.Error("BlindIndex tanpa tenantID harus gagal")
	}
	if _, err := svc.Decrypt(ctx, "", []byte("x")); err == nil {
		t.Error("Decrypt tanpa tenantID harus gagal")
	}
}

// TestPurposeOf mengunci alat yang dipakai lapis repository (PR-3.8.3) untuk menegakkan
// pengikatan KOLOM — sesuatu yang AAD tidak bisa lakukan karena purpose dibaca dari blob
// itu sendiri (lihat fieldAAD). Tanpa pemeriksaan ini, ciphertext bisa dipindah antar kolom
// dalam satu tenant.
func TestPurposeOf(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := PurposeOf(ct)
	if err != nil {
		t.Fatalf("PurposeOf: %v", err)
	}
	if got != "nik" {
		t.Fatalf("purpose = %q, mau \"nik\"", got)
	}

	// Skenario yang harus ditangkap repo: blob purpose "nik" ditemukan di kolom no_rekening.
	// Service sendiri TETAP mendekripsinya (isinya asli, tenant-nya benar) — karena itu
	// pemeriksaan purpose di repo wajib, bukan opsional.
	plain, err := svc.Decrypt(ctx, "pemkot-surabaya", ct)
	if err != nil || string(plain) != "3578010101010001" {
		t.Fatalf("Decrypt = %q err=%v — asumsi test ini keliru bila berubah", plain, err)
	}
	if got == "no_rekening" {
		t.Fatal("purpose tak boleh berubah mengikuti kolom")
	}

	if _, err := PurposeOf([]byte("bukan ciphertext")); !errors.Is(err, port.ErrCiphertextInvalid) {
		t.Fatalf("PurposeOf atas blob asing: err = %v, mau ErrCiphertextInvalid", err)
	}
}

// TestMockCryptoParitasNormalisasi menjaga testkit.MockCrypto tidak lebih longgar daripada
// implementasi nyata: kalau mock mengabaikan case-fold, test keunikan _bidx bisa lolos di
// mock lalu bentrok di produksi. Menambah purpose case-folded di crypto.go WAJIB diikuti di
// testkit — test ini yang menagihnya.
func TestMockCryptoParitasNormalisasi(t *testing.T) {
	svc, _ := newTestService(t)
	mock := testkit.NewMockCrypto()
	ctx := context.Background()

	// Pasangan nilai yang harus dianggap SAMA / BEDA oleh keduanya.
	cases := []struct {
		purpose, a, b string
		wantSama      bool
	}{
		{"email", "Budi@Pemkot.go.id", "budi@pemkot.go.id", true},
		{"email", " budi@pemkot.go.id ", "budi@pemkot.go.id", true},
		{"email", "budi@pemkot.go.id", "siti@pemkot.go.id", false},
		{"nik", " 3578010101010001 ", "3578010101010001", true},
		{"nama_bank", "BRI", "bri", false},
	}

	for _, c := range cases {
		samaAsli := sameBlindIndex(t, svc.BlindIndex, ctx, c.purpose, c.a, c.b)
		samaMock := sameBlindIndex(t, mock.BlindIndex, ctx, c.purpose, c.a, c.b)
		if samaAsli != c.wantSama {
			t.Errorf("Service.BlindIndex(%s, %q vs %q): sama=%v, mau %v", c.purpose, c.a, c.b, samaAsli, c.wantSama)
		}
		if samaMock != samaAsli {
			t.Errorf("MockCrypto menyimpang dari Service untuk purpose %s (%q vs %q): mock=%v service=%v",
				c.purpose, c.a, c.b, samaMock, samaAsli)
		}
	}
}

type blindIndexFunc func(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)

func sameBlindIndex(t *testing.T, fn blindIndexFunc, ctx context.Context, purpose, a, b string) bool {
	t.Helper()
	ha, err := fn(ctx, "pemkot-surabaya", purpose, []byte(a))
	if err != nil {
		t.Fatalf("BlindIndex(%q): %v", a, err)
	}
	hb, err := fn(ctx, "pemkot-surabaya", purpose, []byte(b))
	if err != nil {
		t.Fatalf("BlindIndex(%q): %v", b, err)
	}
	return bytes.Equal(ha, hb)
}

func TestNew_ValidasiRakitan(t *testing.T) {
	provider, err := newLocalProvider(config.CryptoConfig{})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	platform := CustodyProvider{Custody: CustodyPlatform, Driver: DriverLocal, Provider: provider}

	if _, err := New(nil, FixedCustody(CustodyPlatform), 0, platform); err == nil {
		t.Error("New tanpa store harus gagal")
	}
	if _, err := New(newMemDEKStore(), nil, 0, platform); err == nil {
		t.Error("New tanpa CustodyResolver harus gagal")
	}
	if _, err := New(newMemDEKStore(), FixedCustody(CustodyPlatform), 0); err == nil {
		t.Error("New tanpa provider harus gagal (Service tak bisa apa-apa)")
	}
	if _, err := New(newMemDEKStore(), FixedCustody(CustodyPlatform), 0, platform, platform); err == nil {
		t.Error("custody ganda harus gagal")
	}
	if _, err := New(newMemDEKStore(), FixedCustody(CustodyPlatform), 0,
		CustodyProvider{Custody: CustodyPlatform, Provider: provider}); err == nil {
		t.Error("CustodyProvider tanpa nama driver harus gagal (kolom kek_driver butuh nilai)")
	}
}
