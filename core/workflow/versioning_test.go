package workflow_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
)

// ===== MemoryStore: multi-version =====

// defV2 punya ID sama dengan defDisposisi tapi version 2 dan alur BERBEDA: aksi
// "disposisi" tidak ada lagi — dari "diterima" langsung ke "selesai" via "tutup".
// Dipakai untuk membuktikan instance yang terkunci ke v1 tidak terpengaruh v2.
var defV2 = workflow.WorkflowDefinition{
	ID:           defDisposisi.ID,
	Entity:       defDisposisi.Entity,
	Version:      2,
	InitialState: "diterima",
	States: []workflow.State{
		{Name: "diterima", Label: "Diterima", Actions: []string{"tutup"}},
		{Name: "selesai", Label: "Selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{From: "diterima", To: "selesai", On: "tutup"},
	},
	AuthoringSource: "developer",
}

func TestMemoryStore_MultiVersion_GetLatestDanGetVersion(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil { // v1
		t.Fatalf("register v1: %v", err)
	}
	if err := store.Register(defV2); err != nil { // v2
		t.Fatalf("register v2: %v", err)
	}

	// Get → versi terbaru (2).
	latest, err := store.Get(defDisposisi.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if latest.Version != 2 {
		t.Errorf("Get harus kembalikan versi terbaru 2, dapat %d", latest.Version)
	}

	// GetVersion(1) → v1 lama masih ada.
	v1, err := store.GetVersion(defDisposisi.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("GetVersion(1) harus kembalikan versi 1, dapat %d", v1.Version)
	}
	// v1 punya state "didisposisi"; v2 tidak.
	if _, ok := stateNames(v1)["didisposisi"]; !ok {
		t.Error("v1 harusnya masih punya state 'didisposisi'")
	}
}

func TestMemoryStore_GetVersion_TidakAda(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Versi yang tidak ada → error.
	if _, err := store.GetVersion(defDisposisi.ID, 99); err == nil {
		t.Fatal("GetVersion versi tak ada harus error")
	}
	// ID yang tidak ada → error.
	if _, err := store.GetVersion("tidak.ada", 1); err == nil {
		t.Fatal("GetVersion ID tak ada harus error")
	}
}

func TestMemoryStore_VersionNolDinormalkanKeSatu(t *testing.T) {
	store := workflow.NewMemoryStore()
	def := defDisposisi
	def.Version = 0 // belum diset (mis. dari YAML tanpa version)
	if err := store.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := store.GetVersion(defDisposisi.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if got.Version != 1 {
		t.Errorf("version 0 harus dinormalkan ke 1, dapat %d", got.Version)
	}
}

// ===== Instance versioning: DoD PR-3.2.7 =====

// Perubahan definisi setelah instance dimulai TIDAK mengubah instance yang berjalan.
func TestEngine_InstanceTerkunciKeVersiSaatStart(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil { // v1 (punya aksi "disposisi")
		t.Fatalf("register v1: %v", err)
	}
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{})
	ctx := actorDenganPermission(t)

	// Instance dimulai saat hanya v1 ada → terkunci ke v1.
	inst, err := eng.Start(ctx, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if inst.DefinitionVersion != 1 {
		t.Fatalf("instance harus terkunci ke v1, dapat v%d", inst.DefinitionVersion)
	}

	// Admin mengganti definisi → v2 (aksi "disposisi" tidak ada lagi).
	if err := store.Register(defV2); err != nil {
		t.Fatalf("register v2: %v", err)
	}

	// Instance BERJALAN tetap memakai v1: aksi "disposisi" masih valid.
	if err := eng.Execute(ctx, inst, "disposisi", nil); err != nil {
		t.Fatalf("instance berjalan harus tetap pakai v1 (aksi disposisi valid): %v", err)
	}
	if inst.CurrentState != "didisposisi" {
		t.Errorf("state = %q, mau didisposisi (perilaku v1)", inst.CurrentState)
	}
}

// Instance yang dimulai SETELAH v2 mengunci v2 dan melihat alur baru.
func TestEngine_InstanceBaruPakaiVersiTerbaru(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register v1: %v", err)
	}
	if err := store.Register(defV2); err != nil {
		t.Fatalf("register v2: %v", err)
	}
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{})
	ctx := actorDenganPermission(t)

	inst, err := eng.Start(ctx, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if inst.DefinitionVersion != 2 {
		t.Fatalf("instance baru harus terkunci ke v2, dapat v%d", inst.DefinitionVersion)
	}
	// Di v2, aksi "disposisi" tidak ada → ditolak; "tutup" yang valid.
	if err := eng.Execute(ctx, inst, "disposisi", nil); err == nil {
		t.Error("aksi 'disposisi' tidak ada di v2 — harus ditolak")
	}
	if err := eng.Execute(ctx, inst, "tutup", nil); err != nil {
		t.Fatalf("aksi 'tutup' valid di v2: %v", err)
	}
	if inst.CurrentState != "selesai" {
		t.Errorf("state = %q, mau selesai (perilaku v2)", inst.CurrentState)
	}
}

// Instance terkunci ke versi yang dihapus → gagal eksplisit, bukan diam pakai versi lain.
func TestEngine_Execute_VersiTerkunciTidakAda(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register: %v", err)
	}
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{})
	ctx := actorDenganPermission(t)
	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())

	// Simulasikan instance yang mengunci versi yang tak pernah ada di store.
	inst.DefinitionVersion = 7
	if err := eng.Execute(ctx, inst, "disposisi", nil); err == nil {
		t.Fatal("Execute pada versi terkunci yang tidak ada harus gagal eksplisit")
	}
}

// ===== History immutable + komentar =====

func TestEngine_ExecuteWithComment_KomentarMasukHistory(t *testing.T) {
	eng := newEngine(t, guardAlwaysTrue{}, &dispatchRecord{})
	ctx := actorDenganPermission(t)
	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())

	const komentar = "diteruskan ke Kabag Umum"
	if err := eng.ExecuteWithComment(ctx, inst, "disposisi", nil, komentar); err != nil {
		t.Fatalf("ExecuteWithComment: %v", err)
	}
	if len(inst.History) != 1 {
		t.Fatalf("history harus 1 record, dapat %d", len(inst.History))
	}
	if inst.History[0].Comment != komentar {
		t.Errorf("komentar = %q, mau %q", inst.History[0].Comment, komentar)
	}
}

func TestEngine_TransisiGagalTidakMenambahHistory(t *testing.T) {
	// Guard selalu gagal → transisi ditolak → history tidak bertambah, state tetap.
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register: %v", err)
	}
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysFalse{})
	ctx := actorDenganPermission(t)
	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())

	if err := eng.Execute(ctx, inst, "disposisi", nil); err == nil {
		t.Fatal("guard gagal harus menolak transisi")
	}
	if len(inst.History) != 0 {
		t.Errorf("history harus tetap kosong saat transisi gagal, dapat %d", len(inst.History))
	}
	if inst.CurrentState != "diterima" {
		t.Errorf("state harus tetap 'diterima', dapat %q", inst.CurrentState)
	}
}

// stateNames adalah helper: kumpulan nama state suatu definisi.
func stateNames(def workflow.WorkflowDefinition) map[string]struct{} {
	m := make(map[string]struct{}, len(def.States))
	for _, s := range def.States {
		m[s.Name] = struct{}{}
	}
	return m
}
