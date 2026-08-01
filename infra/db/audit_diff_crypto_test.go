package db

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// Penyegelan diff audit diuji lewat sealPair + audit.Diff langsung — pasangan itulah yang
// menentukan isi jejak. Menguji hanya "nilainya bukan plaintext" pernah melewatkan cacat
// sebaliknya: diff yang MENGARANG perubahan (nonce acak membuat nilai sama tampak berbeda)
// dan diff yang MENGHAPUS perubahan (penanda gagal yang sama di kedua sisi).

// failingCrypto meniru KMS/DEK yang sedang gagal: hanya Encrypt yang error, sisanya
// diteruskan ke mock biasa.
type failingCrypto struct{ *testkit.MockCrypto }

func (failingCrypto) Encrypt(context.Context, port.FieldRef, []byte) ([]byte, error) {
	return nil, errors.New("kms sedang gagal")
}

// sealEntityID adalah baris yang dimutasi pada seluruh test di file ini — nilai diff
// terikat padanya (ADR-016), jadi ia harus tetap sama antara segel & buka.
var sealEntityID = uuid.MustParse("11111111-1111-4111-8111-111111111111")

func sealRepo(c port.CryptoPort) *auditedRepo[pegawai] {
	return &auditedRepo[pegawai]{crypto: c, diffEnc: FieldCryptoFromEntity(pegawaiDef())}
}

// sealed menjalankan sealPair atas sepasang snapshot lalu mengembalikan diff-nya.
func sealed(c port.CryptoPort, before, after map[string]any) []audit.FieldDiff {
	sealRepo(c).sealPair(tenantCtx(), sealEntityID, before, after)
	return audit.Diff(before, after)
}

func TestSealPair_KolomTerenkripsiTakBerubahTidakMasukDiff(t *testing.T) {
	diff := sealed(testkit.NewMockCrypto(),
		map[string]any{"nama": "Budi", "nik": "3578010101010001"},
		map[string]any{"nama": "Budi Santoso", "nik": "3578010101010001"},
	)

	if len(diff) != 1 {
		t.Fatalf("hanya nama yang berubah, diff harus 1 field, dapat %d: %+v", len(diff), diff)
	}
	if diff[0].Field != "nama" {
		t.Fatalf("field yang tercatat = %q, mau nama", diff[0].Field)
	}
}

func TestSealPair_TanpaPerubahanSamaSekaliDiffKosong(t *testing.T) {
	// Supresi no-op update di audit.Engine bergantung pada diff kosong; nonce acak sempat
	// mematikannya untuk setiap entity ber-field terenkripsi.
	snap := func() map[string]any {
		return map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	}
	if diff := sealed(testkit.NewMockCrypto(), snap(), snap()); len(diff) != 0 {
		t.Fatalf("snapshot identik harus menghasilkan diff kosong, dapat %+v", diff)
	}
}

func TestSealPair_PerubahanTetapTercatatDanTerenkripsi(t *testing.T) {
	c := testkit.NewMockCrypto()
	const lama, baru = "3578010101010001", "3578010101010002"

	diff := sealed(c,
		map[string]any{"nama": "Budi", "nik": lama},
		map[string]any{"nama": "Budi", "nik": baru},
	)

	if len(diff) != 1 || diff[0].Field != "nik" {
		t.Fatalf("perubahan nik harus tercatat sebagai satu field, dapat %+v", diff)
	}
	for _, sisi := range []struct {
		nama  string
		v     any
		plain string
	}{{"before", diff[0].Before, lama}, {"after", diff[0].After, baru}} {
		s, ok := sisi.v.(string)
		if !ok {
			t.Fatalf("%s bertipe %T, mau string base64", sisi.nama, sisi.v)
		}
		if strings.Contains(s, sisi.plain) {
			t.Fatalf("%s memuat NIK plaintext: %q", sisi.nama, s)
		}
		ct, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("%s bukan base64: %v", sisi.nama, err)
		}
		got, err := c.Decrypt(context.Background(), port.RowRef{
			TenantID: "pemkot-surabaya", RecordID: sealEntityID.String(),
		}, ct)
		if err != nil {
			t.Fatalf("dekripsi %s: %v", sisi.nama, err)
		}
		if string(got) != sisi.plain {
			t.Fatalf("%s membuka menjadi %q, mau %q", sisi.nama, got, sisi.plain)
		}
	}
}

func TestSealPair_EnkripsiGagalTidakMenghapusPerubahan(t *testing.T) {
	// Penanda gagal yang identik di kedua sisi membuat A→B tampak tidak berubah bagi Diff.
	// Bila nik satu-satunya yang berubah, barisnya ter-commit tanpa entry audit sama sekali.
	diff := sealed(failingCrypto{testkit.NewMockCrypto()},
		map[string]any{"nama": "Budi", "nik": "3578010101010001"},
		map[string]any{"nama": "Budi", "nik": "3578010101010002"},
	)

	if len(diff) != 1 || diff[0].Field != "nik" {
		t.Fatalf("perubahan nik harus tetap tercatat meski enkripsi gagal, dapat %+v", diff)
	}
	if diff[0].Before != auditRedactedBefore || diff[0].After != auditRedactedAfter {
		t.Fatalf("penanda kedua sisi harus berbeda, dapat before=%v after=%v", diff[0].Before, diff[0].After)
	}
	for _, v := range []any{diff[0].Before, diff[0].After} {
		if s, _ := v.(string); strings.Contains(s, "3578") {
			t.Fatalf("penanda kegagalan memuat pengenal: %q", s)
		}
	}
}

func TestSealPair_EnkripsiGagalNilaiSamaTetapBukanPerubahan(t *testing.T) {
	diff := sealed(failingCrypto{testkit.NewMockCrypto()},
		map[string]any{"nama": "Budi", "nik": "3578010101010001"},
		map[string]any{"nama": "Budi", "nik": "3578010101010001"},
	)
	if len(diff) != 0 {
		t.Fatalf("nilai sama tak boleh jadi perubahan meski enkripsi gagal, dapat %+v", diff)
	}
}

func TestSealPair_NilaiKosongJadiNilBukanCiphertext(t *testing.T) {
	before := map[string]any{"nama": "Budi", "nik": ""}
	after := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	diff := sealed(testkit.NewMockCrypto(), before, after)

	if len(diff) != 1 || diff[0].Field != "nik" {
		t.Fatalf("pengisian nik harus tercatat, dapat %+v", diff)
	}
	if diff[0].Before != nil {
		t.Fatalf("nilai kosong harus tercatat nil, dapat %v", diff[0].Before)
	}
}

func TestSealPair_SisiKosongCreateDanDelete(t *testing.T) {
	c := testkit.NewMockCrypto()
	after := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	sealRepo(c).sealPair(tenantCtx(), sealEntityID, nil, after) // create: before nil, tak boleh panic
	if s, _ := after["nik"].(string); s == "" || strings.Contains(s, "3578") {
		t.Fatalf("nik pada create harus tersegel, dapat %v", after["nik"])
	}

	before := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	sealRepo(c).sealPair(tenantCtx(), sealEntityID, before, nil) // delete: after nil
	if s, _ := before["nik"].(string); s == "" || strings.Contains(s, "3578") {
		t.Fatalf("nik pada delete harus tersegel, dapat %v", before["nik"])
	}
}

func TestSealPair_TanpaTenantTidakPernahPlaintext(t *testing.T) {
	before := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	after := map[string]any{"nama": "Budi", "nik": "3578010101010002"}
	sealRepo(testkit.NewMockCrypto()).sealPair(context.Background(), sealEntityID, before, after)

	for _, m := range []map[string]any{before, after} {
		if s, _ := m["nik"].(string); strings.Contains(s, "3578") {
			t.Fatalf("tanpa tenant, nik tak boleh tercatat mentah: %q", s)
		}
	}
}

// --- ADR-016: pengikatan nilai diff ke baris ---

func TestSealPair_TanpaEntityIDTidakPernahPlaintext(t *testing.T) {
	// Tanpa id entity, nilai yang disegel tak terikat baris mana pun dan bisa dipindah ke
	// entry audit lain. Yang benar adalah penanda gagal, BUKAN ciphertext tak-terikat —
	// dan tentu bukan plaintext.
	before := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	after := map[string]any{"nama": "Budi", "nik": "3578010101010002"}
	sealRepo(testkit.NewMockCrypto()).sealPair(tenantCtx(), uuid.Nil, before, after)

	if before["nik"] != auditRedactedBefore || after["nik"] != auditRedactedAfter {
		t.Fatalf("tanpa entity id harus jadi penanda gagal, dapat before=%v after=%v", before["nik"], after["nik"])
	}
}

func TestSealPair_NilaiDiffTerikatEntityYangDimutasi(t *testing.T) {
	// Nilai diff milik entry entity A tidak boleh terbuka sebagai milik entity B. Ini yang
	// menutup pemindahan nilai antar entry audit lintas-entity (ADR-016 §Konsekuensi);
	// pemindahan antar entry pada entity yang SAMA dijaga hash chain, bukan kripto.
	c := testkit.NewMockCrypto()
	after := map[string]any{"nama": "Budi", "nik": "3578010101010001"}
	sealRepo(c).sealPair(tenantCtx(), sealEntityID, nil, after)

	ct, err := base64.StdEncoding.DecodeString(after["nik"].(string))
	if err != nil {
		t.Fatalf("nilai tersegel bukan base64: %v", err)
	}
	lain := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	if _, err := c.Decrypt(context.Background(), port.RowRef{
		TenantID: "pemkot-surabaya", RecordID: lain.String(),
	}, ct); !errors.Is(err, port.ErrCiphertextInvalid) {
		t.Fatalf("nilai diff entity lain harus ditolak, err = %v", err)
	}
}
