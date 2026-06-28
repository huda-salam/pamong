//go:build integration

package workflow_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	coreWf "github.com/huda-salam/pamong/core/workflow"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

func newTestStore(t *testing.T) (*infraWf.DBStore, context.Context) {
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
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS gov.workflow_definitions;
			 DROP INDEX IF EXISTS gov.idx_wfdef_lookup`)
		pgpool.Close()
	})
	// Bersihkan state sebelumnya agar test deterministik.
	if _, err := pool.Exec(ctx,
		`DROP TABLE IF EXISTS gov.workflow_definitions;
		 DROP INDEX IF EXISTS gov.idx_wfdef_lookup`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	store := infraWf.NewDBStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return store, ctx
}

// sampleDef membuat WorkflowDefinition sederhana yang valid untuk test.
func sampleDef(id string) coreWf.WorkflowDefinition {
	return coreWf.WorkflowDefinition{
		ID:              id,
		Entity:          "surat_masuk.SuratMasuk",
		Version:         1,
		EffectiveFrom:   time.Now().UTC().Truncate(time.Microsecond),
		InitialState:    "diterima",
		AuthoringSource: "developer",
		States: []coreWf.State{
			{Name: "diterima", Label: "Diterima", IsTerminal: false},
			{Name: "selesai", Label: "Selesai", IsTerminal: true},
		},
		Transitions: []coreWf.Transition{
			{From: "diterima", To: "selesai", On: "selesaikan"},
		},
	}
}

// TestDBStore_RoundTrip: Register → Get mengembalikan definisi yang sama.
func TestDBStore_RoundTrip(t *testing.T) {
	store, ctx := newTestStore(t)
	_ = ctx // EnsureSchema sudah pakai ctx

	def := sampleDef("surat_masuk.disposisi.standar")

	if err := store.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := store.Get(def.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.ID != def.ID {
		t.Errorf("ID: want %q, got %q", def.ID, got.ID)
	}
	if got.Entity != def.Entity {
		t.Errorf("Entity: want %q, got %q", def.Entity, got.Entity)
	}
	if got.InitialState != def.InitialState {
		t.Errorf("InitialState: want %q, got %q", def.InitialState, got.InitialState)
	}
	if got.AuthoringSource != def.AuthoringSource {
		t.Errorf("AuthoringSource: want %q, got %q", def.AuthoringSource, got.AuthoringSource)
	}
	if got.Version != 1 {
		t.Errorf("Version: want 1, got %d", got.Version)
	}
	if len(got.States) != len(def.States) {
		t.Fatalf("States count: want %d, got %d", len(def.States), len(got.States))
	}
	if got.States[0].Name != "diterima" || got.States[1].Name != "selesai" {
		t.Errorf("States: %+v", got.States)
	}
	if len(got.Transitions) != 1 || got.Transitions[0].From != "diterima" {
		t.Errorf("Transitions: %+v", got.Transitions)
	}
	if !got.EffectiveFrom.Equal(def.EffectiveFrom) {
		t.Errorf("EffectiveFrom: want %v, got %v", def.EffectiveFrom, got.EffectiveFrom)
	}
}

// TestDBStore_GetNotFound: Get untuk ID yang tidak ada harus ErrDefinitionNotFound.
func TestDBStore_GetNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	_, err := store.Get("tidak.ada.sama.sekali")
	if err == nil {
		t.Fatal("harus error bila tidak ada")
	}
}

// TestDBStore_IdempotentSeed: SeedYAML dipanggil dua kali untuk ID yang sama —
// baris kedua tidak masuk, tidak ada error, Get tetap mengembalikan versi 1.
func TestDBStore_IdempotentSeed(t *testing.T) {
	store, _ := newTestStore(t)
	def := sampleDef("surat_masuk.disposisi.standar")

	// Seed pertama: masuk.
	if err := store.Register(def); err != nil {
		t.Fatalf("register pertama: %v", err)
	}

	// SeedYAML logic (cek sebelum register): Get ada → skip Register.
	// Simulasikan di sini: cek sendiri lalu panggil Register hanya jika tidak ada.
	if _, err := store.Get(def.ID); err == nil {
		// sudah ada, tidak perlu register ulang
	} else {
		if err2 := store.Register(def); err2 != nil {
			t.Fatalf("register kedua tidak seharusnya jalan: %v", err2)
		}
	}

	// Pastikan versi di DB tetap 1 (tidak ada row baru).
	got, err := store.Get(def.ID)
	if err != nil {
		t.Fatalf("get setelah idempoten: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("versi harus 1 setelah seed idempoten, dapat %d", got.Version)
	}

	// Periksa langsung di DB bahwa hanya ada satu baris.
	// (dilakukan lewat GetVersion — versi 2 tidak boleh ada)
	_, err = store.GetVersion(context.Background(), def.ID, 2)
	if err == nil {
		t.Fatal("versi 2 tidak boleh ada setelah seed idempoten")
	}
}

// TestDBStore_NewVersion: Register dua kali dengan ID sama → dua versi.
// Get mengembalikan versi terbaru (2), GetVersion(1) masih bisa ambil lama.
func TestDBStore_NewVersion(t *testing.T) {
	store, _ := newTestStore(t)
	id := "surat_masuk.disposisi.standar"

	defV1 := sampleDef(id)
	if err := store.Register(defV1); err != nil {
		t.Fatalf("register v1: %v", err)
	}

	// Definisi versi 2: tambah satu state baru.
	defV2 := sampleDef(id)
	defV2.States = append(defV2.States, coreWf.State{Name: "ditolak", Label: "Ditolak", IsTerminal: true})
	defV2.Transitions = append(defV2.Transitions,
		coreWf.Transition{From: "diterima", To: "ditolak", On: "tolak"},
	)
	if err := store.Register(defV2); err != nil {
		t.Fatalf("register v2: %v", err)
	}

	// Get → harus versi 2.
	latest, err := store.Get(id)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest.Version != 2 {
		t.Errorf("versi terbaru harus 2, dapat %d", latest.Version)
	}
	if len(latest.States) != 3 {
		t.Errorf("states versi 2 harus 3, dapat %d", len(latest.States))
	}

	// GetVersion(1) → harus versi 1 dengan 2 states.
	v1, err := store.GetVersion(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("get version 1: %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("GetVersion(1) harus kembalikan versi 1, dapat %d", v1.Version)
	}
	if len(v1.States) != 2 {
		t.Errorf("states versi 1 harus 2, dapat %d", len(v1.States))
	}
}

// TestDBStore_SeedYAML_SkipsExisting: simulasi alur SeedYAML lengkap lewat
// coreWf.SeedYAML — bila DB sudah punya definisi, seed tidak menimpa.
func TestDBStore_SeedYAML_SkipsExisting(t *testing.T) {
	store, _ := newTestStore(t)

	yaml := []byte(`
id: "surat_masuk.disposisi.standar"
entity: "surat_masuk.SuratMasuk"
version: 1
initial_state: diterima
states:
  diterima:
    label: Diterima
    is_terminal: false
  selesai:
    label: Selesai
    is_terminal: true
transitions:
  - from: diterima
    to: selesai
    on: selesaikan
`)

	// Seed pertama: masuk sebagai versi 1.
	if err := coreWf.SeedYAML(yaml, store); err != nil {
		t.Fatalf("seed pertama: %v", err)
	}

	// Seed kedua: SeedYAML harus skip (Get sukses → tidak panggil Register).
	if err := coreWf.SeedYAML(yaml, store); err != nil {
		t.Fatalf("seed kedua tidak boleh error: %v", err)
	}

	// Versi harus tetap 1.
	got, err := store.Get("surat_masuk.disposisi.standar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("versi harus 1 setelah dua kali SeedYAML, dapat %d", got.Version)
	}
}

// TestDBStore_Transition_NotifySpec: Transition dengan NotifySpec di-roundtrip benar.
func TestDBStore_Transition_NotifySpec(t *testing.T) {
	store, _ := newTestStore(t)

	def := sampleDef("surat_masuk.disposisi.standar")
	def.Transitions[0].Notify = &coreWf.NotifySpec{
		ToRole:   "pimpinan",
		Template: "disposisi_selesai",
	}

	if err := store.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := store.Get(def.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Transitions[0].Notify == nil {
		t.Fatal("Notify harus non-nil setelah roundtrip")
	}
	if got.Transitions[0].Notify.ToRole != "pimpinan" {
		t.Errorf("ToRole: want %q, got %q", "pimpinan", got.Transitions[0].Notify.ToRole)
	}
	if got.Transitions[0].Notify.Template != "disposisi_selesai" {
		t.Errorf("Template: want %q, got %q", "disposisi_selesai", got.Transitions[0].Notify.Template)
	}
}
