package crypto

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Test di file ini mengunci KEBIJAKAN yang dulu tersalin di tiap repo tulis-tangan
// (identity, clone tenant): kosong → NULL, pengikatan baris wajib, purpose diperiksa sebelum
// Decrypt. Sejak PR-3.8.5 hanya ada satu implementasinya, jadi hanya ada satu tempat yang
// bisa menyimpang — dan file ini yang menjaganya.

func newTestSealer(t *testing.T, realm string) *FieldSealer {
	t.Helper()
	svc, _ := newTestService(t)
	s, err := NewFieldSealer(svc, realm, "test")
	if err != nil {
		t.Fatalf("NewFieldSealer: %v", err)
	}
	return s
}

func TestFieldSealer_TolakKonstruksiTakLayak(t *testing.T) {
	svc, _ := newTestService(t)

	// CryptoPort nil: kalau ini lolos, pemanggilnya menyimpan pengenal PLAINTEXT tanpa satu
	// pun gejala sampai seseorang membuka dump. Gagalnya harus di konstruksi.
	if _, err := NewFieldSealer(nil, "pemkot-x", "test"); err == nil {
		t.Fatal("CryptoPort nil diterima")
	}
	// Realm kosong menyatukan ruang kunci semua tenant menjadi satu — juga tanpa gejala.
	if _, err := NewFieldSealer(svc, "", "test"); err == nil {
		t.Fatal("realm kosong diterima")
	}
}

func TestFieldSealer_NilaiKosongJadiNULLBukanCiphertext(t *testing.T) {
	s := newTestSealer(t, "pemkot-x")

	enc, bidx, err := s.Seal(context.Background(), "no_hp", uuid.New(), "")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Bukan sekadar kerapian: bidx dari "" adalah SATU nilai konstan yang dibagi semua baris
	// tanpa nilai — ia menumpuk di satu bucket index sekaligus mengumumkan "baris-baris ini
	// sama-sama kosong".
	if enc != nil || bidx != nil {
		t.Fatalf("nilai kosong menghasilkan enc=%v bidx=%v, mau NULL keduanya", enc, bidx)
	}
}

func TestFieldSealer_TanpaIdBarisDitolak(t *testing.T) {
	s := newTestSealer(t, "pemkot-x")

	// Tanpa identitas baris, ciphertext tak terikat ke mana pun dan bisa dipindah diam-diam
	// (ADR-016). Yang dijaga: sealer TIDAK diam-diam memakai id default.
	if _, _, err := s.Seal(context.Background(), "nik", uuid.Nil, "3578010101900001"); err == nil {
		t.Fatal("Seal dengan id baris kosong diterima")
	}
}

func TestFieldSealer_RoundtripDanPengikatanBaris(t *testing.T) {
	s := newTestSealer(t, "pemkot-x")
	ctx := context.Background()
	const nik = "3578010101900001"
	barisA, barisB := uuid.New(), uuid.New()

	enc, bidx, err := s.Seal(ctx, "nik", barisA, nik)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := s.Open(ctx, "nik", barisA, enc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != nik {
		t.Fatalf("Open = %q, mau %q", got, nik)
	}

	// Ciphertext yang dipindah ke BARIS lain harus gagal dibuka (ADR-016).
	if _, err := s.Open(ctx, "nik", barisB, enc); err == nil {
		t.Fatal("ciphertext baris A terbuka di baris B")
	}

	// Blind index WAJIB row-independent, kalau tidak `WHERE nik_bidx = $1` tak pernah cocok.
	lookup, err := s.Index(ctx, "nik", nik)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if string(lookup) != string(bidx) {
		t.Fatal("blind index ikut ber-baris — lookup & UNIQUE mati tanpa error")
	}
}

func TestFieldSealer_CiphertextKolomLainDitolak(t *testing.T) {
	s := newTestSealer(t, "pemkot-x")
	ctx := context.Background()
	baris := uuid.New()

	// AAD hanya mengikat realm & baris, jadi tanpa pemeriksaan purpose (ADR-015) blob no_hp
	// bisa disalin ke kolom email pada BARIS YANG SAMA dan tetap terbuka.
	encNoHP, _, err := s.Seal(ctx, "no_hp", baris, "081200000000")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	_, err = s.Open(ctx, "email", baris, encNoHP)
	if err == nil {
		t.Fatal("blob no_hp terbuka sebagai kolom email")
	}
	if !strings.Contains(err.Error(), "ADR-015") {
		t.Fatalf("error tak menunjuk pemeriksaan purpose: %v", err)
	}
}

func TestFieldSealer_RealmMemisahkanRuangKunci(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	baris := uuid.New()
	const nik = "3578010101900001"

	a, err := NewFieldSealer(svc, "pemkot-a", "test")
	if err != nil {
		t.Fatalf("NewFieldSealer: %v", err)
	}
	b, err := NewFieldSealer(svc, "pemkot-b", "test")
	if err != nil {
		t.Fatalf("NewFieldSealer: %v", err)
	}

	enc, bidxA, err := a.Seal(ctx, "nik", baris, nik)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Dump satu tenant tak bisa dibuka dengan kunci tenant lain — inilah yang membuat clone
	// per-tenant tetap terisolasi meski isinya person yang sama.
	if _, err := b.Open(ctx, "nik", baris, enc); err == nil {
		t.Fatal("ciphertext realm A terbuka di realm B")
	}
	bidxB, err := b.Index(ctx, "nik", nik)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	// Konsekuensi yang disengaja: bidx tak bisa dipakai mencocokkan orang LINTAS tenant.
	if string(bidxA) == string(bidxB) {
		t.Fatal("blind index sama antar realm — korelasi lintas tenant terbuka")
	}
}

// TestFieldSealer_RealmDipatriBukanPerPanggilan menjaga agar realm tak bisa diselipkan
// pemanggil: satu-satunya sumbernya adalah konstruktor. Realm yang salah TIDAK gagal — ia
// hanya menghasilkan bidx yang tak pernah cocok dan ciphertext yang tak terbuka lagi.
func TestFieldSealer_RealmDipatriBukanPerPanggilan(t *testing.T) {
	s := newTestSealer(t, "pemkot-x")
	if s.Realm() != "pemkot-x" {
		t.Fatalf("Realm() = %q", s.Realm())
	}
}
