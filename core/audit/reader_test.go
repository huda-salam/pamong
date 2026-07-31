package audit_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/testkit"
)

// fakeQueryStore mengembalikan entry yang disiapkan test, dan mencatat argumen yang diterima
// agar bisa dibuktikan tenant tidak pernah datang dari parameter pemanggil.
type fakeQueryStore struct {
	entries     []audit.AuditEntry
	tenantAsked string
}

func (f *fakeQueryStore) ByEntity(_ context.Context, _ string, _ uuid.UUID) ([]audit.AuditEntry, error) {
	return f.entries, nil
}

func (f *fakeQueryStore) ByTenant(_ context.Context, tenantID string) ([]audit.AuditEntry, error) {
	f.tenantAsked = tenantID
	return f.entries, nil
}

// encB64 menyiapkan nilai diff sebagaimana ditulis lapis repository: ciphertext base64.
func encB64(t *testing.T, tenantID, purpose, plain string) string {
	t.Helper()
	ct, err := (&testkit.MockCrypto{}).Encrypt(context.Background(), tenantID, purpose, []byte(plain))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return base64.StdEncoding.EncodeToString(ct)
}

func entryDenganNIK(t *testing.T, tenantID string) audit.AuditEntry {
	t.Helper()
	return audit.AuditEntry{
		ID: uuid.New(), TenantID: tenantID, Entity: "kepegawaian.Pegawai", EntityID: uuid.New(),
		Action: audit.ActionUpdate, ActorID: uuid.New(), Timestamp: time.Now(),
		Diff: []audit.FieldDiff{
			{Field: "nama", Before: "Budi", After: "Budi Santoso"},
			{
				Field:  "nik",
				Before: encB64(t, tenantID, "nik", "3578010101010001"),
				After:  encB64(t, tenantID, "nik", "3578010101019999"),
			},
		},
		PrevHash: "prev", Hash: "hash",
	}
}

func TestReader_TanpaPermission_NilaiSensitifTertutup(t *testing.T) {
	store := &fakeQueryStore{entries: []audit.AuditEntry{entryDenganNIK(t, "pemkot-surabaya")}}
	r := audit.NewReader(store, &testkit.MockCrypto{})
	actx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"))

	got, err := r.ByTenant(actx)
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entry = %d, mau 1 — entry tidak boleh ikut disembunyikan", len(got))
	}
	nik := diffOf(t, got[0].Diff, "nik")
	if nik.Before != audit.HiddenSensitive || nik.After != audit.HiddenSensitive {
		t.Fatalf("nilai nik = %v -> %v, mau tertutup", nik.Before, nik.After)
	}
	// Sisa jejak tetap terbaca: siapa, kapan, field apa yang berubah.
	nama := diffOf(t, got[0].Diff, "nama")
	if nama.Before != "Budi" || nama.After != "Budi Santoso" {
		t.Fatalf("nilai non-sensitif ikut tertutup: %v -> %v", nama.Before, nama.After)
	}
	if got[0].ActorID == uuid.Nil || got[0].Action != audit.ActionUpdate {
		t.Fatalf("metadata entry hilang: %+v", got[0])
	}
}

func TestReader_DenganPermission_NilaiSensitifTerbuka(t *testing.T) {
	store := &fakeQueryStore{entries: []audit.AuditEntry{entryDenganNIK(t, "pemkot-surabaya")}}
	r := audit.NewReader(store, &testkit.MockCrypto{})
	actx := testkit.Ctx(t,
		testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(audit.PermSensitiveBaca))

	got, err := r.ByTenant(actx)
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	nik := diffOf(t, got[0].Diff, "nik")
	if nik.Before != "3578010101010001" || nik.After != "3578010101019999" {
		t.Fatalf("nilai nik = %v -> %v, mau terdekripsi", nik.Before, nik.After)
	}
}

// TestReader_NilaiBiasaTidakDisentuh mengunci batas gating: hanya ciphertext framework yang
// diperlakukan sensitif. String base64 yang kebetulan sah (dan angka, bool, nil) harus lewat
// apa adanya — bila tidak, audit field biasa akan tampak tertutup tanpa alasan.
func TestReader_NilaiBiasaTidakDisentuh(t *testing.T) {
	e := audit.AuditEntry{
		ID: uuid.New(), TenantID: "t", Entity: "x.Y", EntityID: uuid.New(),
		Action: audit.ActionUpdate, Timestamp: time.Now(),
		Diff: []audit.FieldDiff{
			{Field: "jumlah", Before: 1000, After: 2000},
			{Field: "aktif", Before: true, After: false},
			{Field: "catatan", Before: nil, After: "revisi"},
			{Field: "kode", Before: "", After: base64.StdEncoding.EncodeToString([]byte("bukan ciphertext"))},
		},
	}
	r := audit.NewReader(&fakeQueryStore{entries: []audit.AuditEntry{e}}, &testkit.MockCrypto{})

	got, err := r.ByTenant(testkit.Ctx(t, testkit.WithTenant("t")))
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	for i, d := range got[0].Diff {
		if d.Before != e.Diff[i].Before || d.After != e.Diff[i].After {
			t.Errorf("field %s berubah: %v->%v jadi %v->%v", d.Field,
				e.Diff[i].Before, e.Diff[i].After, d.Before, d.After)
		}
	}
}

// TestReader_TenantSelaluDariToken: tenant tak pernah datang dari parameter pemanggil, dan
// entry milik tenant lain yang terlanjur terbaca store disaring.
func TestReader_TenantSelaluDariToken(t *testing.T) {
	milikOrangLain := entryDenganNIK(t, "pemkot-malang")
	store := &fakeQueryStore{entries: []audit.AuditEntry{
		entryDenganNIK(t, "pemkot-surabaya"), milikOrangLain,
	}}
	r := audit.NewReader(store, &testkit.MockCrypto{})
	actx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(audit.PermSensitiveBaca))

	if _, err := r.ByTenant(actx); err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	if store.tenantAsked != "pemkot-surabaya" {
		t.Fatalf("store ditanyai tenant %q, mau dari token", store.tenantAsked)
	}

	got, err := r.ByEntity(actx, milikOrangLain.Entity, milikOrangLain.EntityID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	for _, e := range got {
		if e.TenantID != "pemkot-surabaya" {
			t.Fatalf("entry tenant %q lolos ke pembaca tenant lain", e.TenantID)
		}
	}
}

// TestReader_CiphertextTakTerbukaTidakJadiBlobMentah: kegagalan dekripsi (mis. kunci tenant
// lain) harus tampil sebagai penanda, bukan blob yang menyamar sebagai nilai.
func TestReader_CiphertextTakTerbukaTidakJadiBlobMentah(t *testing.T) {
	e := entryDenganNIK(t, "pemkot-surabaya")
	// Ciphertext dibuat untuk tenant lain, tapi entry-nya milik tenant ini.
	e.Diff[1].Before = encB64(t, "pemkot-malang", "nik", "3578010101010001")
	store := &fakeQueryStore{entries: []audit.AuditEntry{e}}
	r := audit.NewReader(store, &testkit.MockCrypto{})

	got, err := r.ByTenant(testkit.Ctx(t,
		testkit.WithTenant("pemkot-surabaya"), testkit.WithPermission(audit.PermSensitiveBaca)))
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	nik := diffOf(t, got[0].Diff, "nik")
	if nik.Before != audit.UndecryptableRaw {
		t.Fatalf("nilai gagal dekripsi = %v, mau penanda", nik.Before)
	}
	if s, ok := nik.Before.(string); ok && strings.Contains(s, "mock:") {
		t.Fatal("blob mentah bocor ke hasil baca")
	}
}

// TestReader_TanpaCryptoNilaiTetapTertutup: deployment tanpa CryptoPort tak bisa membuka
// apa pun — nilai tersimpan tetap base64 buram, bukan plaintext.
func TestReader_TanpaCryptoNilaiTetapTertutup(t *testing.T) {
	e := entryDenganNIK(t, "pemkot-surabaya")
	r := audit.NewReader(&fakeQueryStore{entries: []audit.AuditEntry{e}}, nil)

	got, err := r.ByTenant(testkit.Ctx(t,
		testkit.WithTenant("pemkot-surabaya"), testkit.WithPermission(audit.PermSensitiveBaca)))
	if err != nil {
		t.Fatalf("byTenant: %v", err)
	}
	nik := diffOf(t, got[0].Diff, "nik")
	s, _ := nik.Before.(string)
	if strings.Contains(s, "3578010101010001") {
		t.Fatal("NIK terbaca tanpa CryptoPort")
	}
}

func diffOf(t *testing.T, diffs []audit.FieldDiff, field string) audit.FieldDiff {
	t.Helper()
	for _, d := range diffs {
		if d.Field == field {
			return d
		}
	}
	t.Fatalf("field %q tidak ada di diff %+v", field, diffs)
	return audit.FieldDiff{}
}
