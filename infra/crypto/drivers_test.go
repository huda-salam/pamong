package crypto

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/config"
)

func masterKey(b byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}

func testRef() KeyRef {
	return KeyRef{TenantID: "pemkot-surabaya", Purpose: "nik", Kind: KindEncryption, Custody: CustodyPlatform}
}

func TestStatic_MenolakStartTanpaMasterKeyValid(t *testing.T) {
	cases := map[string]config.CryptoConfig{
		"tanpa master key":  {},
		"bukan base64":      {MasterKey: "bukan base64!!"},
		"panjang bukan 32B": {MasterKey: base64.StdEncoding.EncodeToString([]byte("terlalu pendek"))},
	}
	for name, cfg := range cases {
		if _, err := newStaticProvider(cfg); err == nil {
			t.Errorf("%s: driver static harus menolak start", name)
		}
	}

	if _, err := newStaticProvider(config.CryptoConfig{}); !errors.Is(err, ErrMasterKeyRequired) {
		t.Errorf("tanpa master key: err = %v, mau ErrMasterKeyRequired", err)
	}
}

func TestStatic_WrapUnwrapRoundtrip(t *testing.T) {
	p, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x11)})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ctx := context.Background()
	ref := testRef()

	dek, wrapped, err := p.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}
	if len(dek) != dekLen {
		t.Fatalf("panjang DEK = %d, mau %d", len(dek), dekLen)
	}
	if bytes.Contains(wrapped, dek) {
		t.Fatal("blob ter-wrap memuat DEK mentah")
	}

	got, err := p.UnwrapDEK(ctx, ref, wrapped)
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("DEK hasil unwrap berbeda")
	}
}

func TestStatic_BlobTerikatKeKeyRef(t *testing.T) {
	p, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x22)})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ctx := context.Background()
	ref := testRef()
	_, wrapped, err := p.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}

	// Memindahkan baris data_keys ke tenant/purpose/kind lain tidak boleh membukanya —
	// inilah yang menjaga isolasi meski penyerang punya akses tulis ke tabel kunci.
	beda := map[string]func(KeyRef) KeyRef{
		"tenant lain":  func(r KeyRef) KeyRef { r.TenantID = "pemkot-malang"; return r },
		"purpose lain": func(r KeyRef) KeyRef { r.Purpose = "no_rekening"; return r },
		"kind lain":    func(r KeyRef) KeyRef { r.Kind = KindBlindIndex; return r },
		"custody lain": func(r KeyRef) KeyRef { r.Custody = CustodyTenant; return r },
	}
	for name, mutate := range beda {
		if _, err := p.UnwrapDEK(ctx, mutate(ref), wrapped); err == nil {
			t.Errorf("%s: unwrap seharusnya gagal", name)
		}
	}
}

func TestStatic_RotasiMasterKeyV1KeV2(t *testing.T) {
	ctx := context.Background()
	ref := testRef()

	// Sebelum rotasi: hanya V1.
	v1, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x33)})
	if err != nil {
		t.Fatalf("provider v1: %v", err)
	}
	dekLama, wrappedLama, err := v1.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK v1: %v", err)
	}

	// Setelah rotasi: V2 ditambahkan, V1 TETAP ADA (syarat membuka DEK lama).
	rotated, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x33), MasterKeyV2: masterKey(0x44)})
	if err != nil {
		t.Fatalf("provider rotasi: %v", err)
	}

	// DEK lama tetap terbuka (dibungkus V1).
	got, err := rotated.UnwrapDEK(ctx, ref, wrappedLama)
	if err != nil {
		t.Fatalf("unwrap DEK lama setelah rotasi: %v", err)
	}
	if !bytes.Equal(got, dekLama) {
		t.Fatal("DEK lama salah setelah rotasi")
	}

	// DEK baru dibungkus dengan versi aktif = tertinggi = 2.
	_, wrappedBaru, err := rotated.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK setelah rotasi: %v", err)
	}
	if wrappedBaru[1] != 2 {
		t.Fatalf("versi KEK pembungkus = %d, mau 2", wrappedBaru[1])
	}

	// Menghapus V1 setelah rotasi membuat DEK lama tak terbuka — pesan errornya wajib
	// menyebut versi yang hilang agar ops tahu kunci mana yang harus dipulihkan.
	hanyaV2, err := newStaticProvider(config.CryptoConfig{MasterKeyV2: masterKey(0x44)})
	if err != nil {
		t.Fatalf("provider hanya v2: %v", err)
	}
	_, err = hanyaV2.UnwrapDEK(ctx, ref, wrappedLama)
	if err == nil || !strings.Contains(err.Error(), "versi 1") {
		t.Fatalf("err = %v, mau menyebut versi 1 yang hilang", err)
	}
}

func TestKekWrapper_BlobRusak(t *testing.T) {
	p, err := newStaticProvider(config.CryptoConfig{MasterKey: masterKey(0x55)})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	ctx := context.Background()
	ref := testRef()
	_, wrapped, err := p.GenerateDEK(ctx, ref)
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}

	tampered := append([]byte(nil), wrapped...)
	tampered[len(tampered)-1] ^= 0xFF
	asing := append([]byte(nil), wrapped...)
	asing[0] = 0x09

	for name, blob := range map[string][]byte{
		"terpotong":         wrapped[:wrapHeaderLen],
		"format asing":      asing,
		"tag dimodifikasi":  tampered,
		"versi KEK tak ada": append([]byte{wrapFormatV1, 9}, wrapped[2:]...),
	} {
		if _, err := p.UnwrapDEK(ctx, ref, blob); err == nil {
			t.Errorf("%s: unwrap seharusnya gagal", name)
		}
	}
}

func TestRegistry_DriverBawaanTerdaftar(t *testing.T) {
	got := RegisteredProviders()
	for _, want := range []string{DriverLocal, DriverStatic} {
		if !containsString(got, want) {
			t.Errorf("driver %q tidak terdaftar (terdaftar: %v)", want, got)
		}
	}
}

func TestNewProvider_DriverTakDikenal(t *testing.T) {
	_, err := NewProvider("kms-yang-belum-ada", config.CryptoConfig{})
	if err == nil {
		t.Fatal("driver tak dikenal harus error saat boot, bukan saat baris pertama dienkripsi")
	}
	// Pesan menyebut pilihan yang ada agar typo config cepat ketemu.
	if !strings.Contains(err.Error(), DriverStatic) {
		t.Errorf("pesan error tidak menyebut driver terdaftar: %v", err)
	}
}

// TestRegisterProvider_DriverBaruTanpaUbahKodeKripto mengunci janji ADR-010: menambah KMS =
// satu implementasi + Register, tanpa menyentuh kode kripto.
func TestRegisterProvider_DriverBaruTanpaUbahKodeKripto(t *testing.T) {
	const name = "kms-uji"
	RegisterProvider(name, func(config.CryptoConfig) (KeyProvider, error) {
		// Provider palsu: cukup memenuhi interface untuk membuktikan seam-nya bekerja.
		return kekWrapper{driver: name, keys: map[int][]byte{1: bytes.Repeat([]byte{0x66}, 32)}, active: 1}, nil
	})
	t.Cleanup(func() {
		providersMu.Lock()
		delete(providers, name)
		providersMu.Unlock()
	})

	p, err := NewProvider(name, config.CryptoConfig{})
	if err != nil {
		t.Fatalf("NewProvider driver baru: %v", err)
	}

	store := newMemDEKStore()
	svc, err := New(store, FixedCustody(CustodyPlatform), 0,
		CustodyProvider{Custody: CustodyPlatform, Driver: name, Provider: p})
	if err != nil {
		t.Fatalf("New dengan driver baru: %v", err)
	}
	ctx := context.Background()
	ct, err := svc.Encrypt(ctx, "pemkot-surabaya", "nik", []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	plain, err := svc.Decrypt(ctx, "pemkot-surabaya", ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "3578010101010001" {
		t.Fatalf("plaintext = %q", plain)
	}
	// kek_driver ikut tercatat — diagnosa saat KMS diganti.
	rec, ok, _ := store.Active(ctx, testRef())
	if !ok || rec.KEKDriver != name {
		t.Fatalf("kek_driver tersimpan = %q, mau %q", rec.KEKDriver, name)
	}
}

func TestRegisterProvider_NamaGandaPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("mendaftarkan nama driver yang sama dua kali harus panic (kesalahan program)")
		}
	}()
	RegisterProvider(DriverStatic, newStaticProvider)
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
