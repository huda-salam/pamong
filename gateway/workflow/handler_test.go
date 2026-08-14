package workflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	gatewaywf "github.com/huda-salam/pamong/gateway/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

const (
	tenantUji = "pemkot-surabaya"
	slotUji   = "surat_masuk.disposisi"
)

// --- fixture ---

var defUji = coreWf.WorkflowDefinition{
	ID:            "surat_masuk.disposisi.standar",
	Entity:        "surat_masuk.SuratMasuk",
	Version:       1,
	EffectiveFrom: time.Now(),
	InitialState:  "diterima",
	States: []coreWf.State{
		{Name: "diterima", Actions: []string{"disposisi"}},
		{Name: "didisposisi", Actions: []string{"selesai"}},
		{Name: "selesai", IsTerminal: true},
	},
	Transitions: []coreWf.Transition{
		{From: "diterima", To: "didisposisi", On: "disposisi", Action: "DisposisiSurat"},
		{From: "didisposisi", To: "selesai", On: "selesai"},
	},
	AuthoringSource: "developer",
}

// actionSpy merekam params yang benar-benar sampai ke action lewat seluruh rantai HTTP→engine.
type actionSpy struct{ last port.WorkflowActionInput }

func (a *actionSpy) RunWorkflowAction(_ port.AuthContext, in port.WorkflowActionInput) error {
	a.last = in
	return nil
}

// stubProvider menyerahkan satu Runtime untuk tenant apa pun — cukup untuk menguji handler.
type stubProvider struct {
	rt  gatewaywf.Runtime
	err error
}

func (s stubProvider) RuntimeFor(context.Context, string) (gatewaywf.Runtime, error) {
	return s.rt, s.err
}

type stubEvaluator struct{ granted map[string]bool }

func (s stubEvaluator) Allows(_ []port.RoleRef, perm string) bool { return s.granted[perm] }

// newRuntime merakit tumpukan workflow in-memory: definisi + pilihan template tenant + store
// instance. Sengaja memakai komponen NYATA (MemoryStore, MemoryTemplateStore, Engine) dan hanya
// men-stub batas luar — yang diuji adalah rantai handler→engine→dispatcher, bukan handler sendiri.
func newRuntime(t *testing.T, spy *actionSpy) gatewaywf.Runtime {
	t.Helper()
	defs := coreWf.NewMemoryStore()
	if err := defs.Register(defUji); err != nil {
		t.Fatalf("register definisi: %v", err)
	}
	templates := coreWf.NewMemoryTemplateStore(defs)
	if err := templates.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   tenantUji,
		Slot:       slotUji,
		TemplateID: defUji.ID,
	}); err != nil {
		t.Fatalf("set template tenant: %v", err)
	}
	actions := coreWf.NewActionRegistry()
	if err := actions.RegisterAction("DisposisiSurat", spy); err != nil {
		t.Fatalf("register action: %v", err)
	}
	engine := coreWf.New(defs, actions, coreWf.NewGuardEvaluator(), coreWf.WithTemplates(templates))
	return gatewaywf.Runtime{Engine: engine, Instances: testkit.NewMemoryInstanceStore()}
}

// req membangun request ber-Context otentikasi lengkap (tenant + permission), seperti yang
// dihasilkan middleware auth di produksi.
func req(t *testing.T, method, target, body string, perms ...string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	granted := make(map[string]bool, len(perms))
	for _, p := range perms {
		granted[p] = true
	}
	ctx := gateway.NewContextFromClaims(r.Context(), &port.Claims{
		PersonID:    uuid.New(),
		Persona:     "employee",
		TenantID:    tenantUji,
		TenantRoles: []string{"operator_surat"},
	})
	ctx.SetPermissionEvaluator(stubEvaluator{granted: granted})
	return gateway.WithContext(r, ctx)
}

// serve menjalankan request lewat router NYATA agar pola rute ({id}) ikut teruji — PathValue
// hanya terisi bila request melewati ServeMux, jadi memanggil handler langsung akan menyembunyikan
// pola rute yang salah.
func serve(t *testing.T, h *gatewaywf.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	router := gateway.NewRouter()
	gatewaywf.MountRoutes(router, h)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body bukan JSON: %v (%s)", err, w.Body.String())
	}
	return out
}

// --- test ---

// Alur penuh: start dari template tenant → transisi → action menerima params & entity_id.
func TestHandler_StartLaluTransisi(t *testing.T) {
	spy := &actionSpy{}
	h := gatewaywf.NewHandler(stubProvider{rt: newRuntime(t, spy)})
	entityID := uuid.New()

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances",
		`{"slot":"`+slotUji+`","entity_id":"`+entityID.String()+`"}`,
		coreWf.PermInstanceMulai))
	if w.Code != http.StatusCreated {
		t.Fatalf("start: status %d, ingin 201 (%s)", w.Code, w.Body.String())
	}
	started := decodeBody(t, w)
	if started["current_state"] != "diterima" {
		t.Fatalf("current_state = %v, ingin diterima", started["current_state"])
	}
	instID, _ := started["id"].(string)

	w = serve(t, h, req(t, http.MethodPost, "/workflow/instances/"+instID+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum"},"comment":"segera"}`,
		coreWf.PermInstanceTransisi))
	if w.Code != http.StatusOK {
		t.Fatalf("transisi: status %d, ingin 200 (%s)", w.Code, w.Body.String())
	}
	after := decodeBody(t, w)
	if after["current_state"] != "didisposisi" {
		t.Fatalf("current_state = %v, ingin didisposisi", after["current_state"])
	}

	// Action menerima entitas dari INSTANCE (bukan dari body) beserta params aktor.
	if spy.last.EntityID != entityID {
		t.Errorf("EntityID action = %v, ingin %v", spy.last.EntityID, entityID)
	}
	if spy.last.Params["kepada_jabatan"] != "kabag_umum" {
		t.Errorf("params action = %+v", spy.last.Params)
	}
	if spy.last.TenantID != tenantUji {
		t.Errorf("TenantID action = %q, ingin %q", spy.last.TenantID, tenantUji)
	}

	// Riwayat tersimpan & terbaca lewat GET.
	w = serve(t, h, req(t, http.MethodGet, "/workflow/instances/"+instID, "", coreWf.PermInstanceBaca))
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d, ingin 200 (%s)", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	history, _ := got["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history = %v, ingin 1 entri", got["history"])
	}
	rec, _ := history[0].(map[string]any)
	if rec["comment"] != "segera" || rec["to"] != "didisposisi" {
		t.Errorf("entri riwayat tak sesuai: %+v", rec)
	}
}

// Tanpa permission runtime workflow, permintaan ditolak SEBELUM apa pun dibaca/dikerjakan.
func TestHandler_PermissionDenied(t *testing.T) {
	spy := &actionSpy{}
	h := gatewaywf.NewHandler(stubProvider{rt: newRuntime(t, spy)})

	cases := []struct {
		nama, method, target, body string
	}{
		{"start", http.MethodPost, "/workflow/instances", `{"slot":"` + slotUji + `","entity_id":"` + uuid.New().String() + `"}`},
		{"transisi", http.MethodPost, "/workflow/instances/" + uuid.New().String() + "/transitions", `{"action":"disposisi"}`},
		{"baca", http.MethodGet, "/workflow/instances/" + uuid.New().String(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.nama, func(t *testing.T) {
			// Permission LAIN diberikan, bukan tak ada sama sekali: itu membedakan "ditolak
			// karena permission yang tepat tak dimiliki" dari "ditolak karena tak punya apa pun".
			w := serve(t, h, req(t, tc.method, tc.target, tc.body, "workflow:template:pilih"))
			if w.Code != http.StatusForbidden {
				t.Fatalf("status %d, ingin 403 (%s)", w.Code, w.Body.String())
			}
		})
	}
	if spy.last.Action != "" {
		t.Error("action dipanggil meski permission ditolak")
	}
}

// Instance milik tenant lain dijawab 404 — bukan 403, dan bukan datanya. Menjawab 403 sudah
// membocorkan bahwa ID itu ada.
func TestHandler_InstanceTenantLainTidakTerlihat(t *testing.T) {
	rt := newRuntime(t, &actionSpy{})
	asing := &coreWf.WorkflowInstance{
		ID:                uuid.New(),
		TenantID:          "pemkot-malang",
		DefinitionID:      defUji.ID,
		DefinitionVersion: 1,
		EntityID:          uuid.New(),
		CurrentState:      "diterima",
		StartedAt:         time.Now(),
	}
	if err := rt.Instances.Save(context.Background(), asing); err != nil {
		t.Fatalf("seed instance tenant lain: %v", err)
	}
	h := gatewaywf.NewHandler(stubProvider{rt: rt})

	w := serve(t, h, req(t, http.MethodGet, "/workflow/instances/"+asing.ID.String(), "",
		coreWf.PermInstanceBaca))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, ingin 404 (%s)", w.Code, w.Body.String())
	}
}

// Slot yang belum dipilih tenant → 404 dengan pesan template, bukan 500.
func TestHandler_SlotBelumDikonfigurasi(t *testing.T) {
	h := gatewaywf.NewHandler(stubProvider{rt: newRuntime(t, &actionSpy{})})

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances",
		`{"slot":"tak.ada","entity_id":"`+uuid.New().String()+`"}`, coreWf.PermInstanceMulai))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, ingin 404 (%s)", w.Code, w.Body.String())
	}
}

// Transisi yang tak sah dari state sekarang ditolak, dan instance TIDAK berubah.
func TestHandler_TransisiIlegalTidakMengubahInstance(t *testing.T) {
	spy := &actionSpy{}
	rt := newRuntime(t, spy)
	h := gatewaywf.NewHandler(stubProvider{rt: rt})
	entityID := uuid.New()

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances",
		`{"slot":"`+slotUji+`","entity_id":"`+entityID.String()+`"}`, coreWf.PermInstanceMulai))
	instID, _ := decodeBody(t, w)["id"].(string)

	w = serve(t, h, req(t, http.MethodPost, "/workflow/instances/"+instID+"/transitions",
		`{"action":"selesai"}`, coreWf.PermInstanceTransisi))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, ingin 422 (%s)", w.Code, w.Body.String())
	}

	w = serve(t, h, req(t, http.MethodGet, "/workflow/instances/"+instID, "", coreWf.PermInstanceBaca))
	if state := decodeBody(t, w)["current_state"]; state != "diterima" {
		t.Fatalf("current_state = %v setelah transisi ilegal, ingin tetap diterima", state)
	}
}

// --- Regresi review PR-W4a ---

// Transisi yang datang saat transisi LAIN atas instance yang sama sedang berjalan harus ditolak
// SEBELUM action-nya dipanggil.
//
// Optimistic locking pada Save tidak cukup untuk ini: ia menolak penulis yang kalah SESUDAH
// action-nya berjalan, jadi yang terlindungi hanya baris instance — bukan efek bisnisnya (dua
// baris disposisi, dua event, untuk satu surat).
func TestHandler_TransisiSaatInstanceTerkunciDitolakSebelumAction(t *testing.T) {
	spy := &actionSpy{}
	rt := newRuntime(t, spy)
	h := gatewaywf.NewHandler(stubProvider{rt: rt})
	entityID := uuid.New()

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances",
		`{"slot":"`+slotUji+`","entity_id":"`+entityID.String()+`"}`, coreWf.PermInstanceMulai))
	if w.Code != http.StatusCreated {
		t.Fatalf("start: %d — %s", w.Code, w.Body.String())
	}
	instID, _ := decodeBody(t, w)["id"].(string)
	id := uuid.MustParse(instID)

	// Transisi lain sedang berjalan atas instance ini.
	release, locked, err := rt.Instances.TryLockInstance(context.Background(), id)
	if err != nil || !locked {
		t.Fatalf("gagal memegang kunci instance: locked=%v err=%v", locked, err)
	}

	w = serve(t, h, req(t, http.MethodPost, "/workflow/instances/"+instID+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum"}}`,
		coreWf.PermInstanceTransisi))

	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, ingin 409 (%s)", w.Code, w.Body.String())
	}
	if spy.last.Action != "" {
		t.Fatal("action dijalankan meski instance sedang dikunci transisi lain — efek bisnis ganda")
	}

	// Setelah dilepas, transisi yang sama berhasil — kunci tidak bocor.
	release()
	w = serve(t, h, req(t, http.MethodPost, "/workflow/instances/"+instID+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum"}}`,
		coreWf.PermInstanceTransisi))
	if w.Code != http.StatusOK {
		t.Fatalf("setelah kunci dilepas: status %d, ingin 200 (%s)", w.Code, w.Body.String())
	}
	if spy.last.Action != "DisposisiSurat" {
		t.Fatal("action tak dipanggil setelah kunci dilepas — kunci bocor")
	}
}

// notifierGagal meniru kegagalan SESUDAH transisi otoritatif (PR-W4b memasang notifier nyata).
type notifierGagal struct{}

func (notifierGagal) NotifyTransition(context.Context, string, coreWf.NotifySpec, coreWf.WorkflowInstance) error {
	return core.ErrUnavailable("hub notifikasi tak terjangkau")
}

// Transisi yang SUDAH terjadi wajib tersimpan meski langkah sesudahnya (SLA/notifikasi) gagal.
// Membuangnya berarti kehilangan transisi yang efek bisnisnya sudah terjadi — dan action-nya
// bisa dijalankan ulang oleh percobaan berikutnya.
func TestHandler_GagalSesudahTransisiTetapDisimpan(t *testing.T) {
	spy := &actionSpy{}
	defs := coreWf.NewMemoryStore()
	defBernotify := defUji
	defBernotify.Transitions = []coreWf.Transition{{
		From: "diterima", To: "didisposisi", On: "disposisi", Action: "DisposisiSurat",
		Notify: &coreWf.NotifySpec{ToRole: "agendaris", Template: "surat_didisposisi"},
	}}
	if err := defs.Register(defBernotify); err != nil {
		t.Fatalf("register definisi: %v", err)
	}
	templates := coreWf.NewMemoryTemplateStore(defs)
	if err := templates.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID: tenantUji, Slot: slotUji, TemplateID: defBernotify.ID,
	}); err != nil {
		t.Fatalf("set template: %v", err)
	}
	actions := coreWf.NewActionRegistry()
	if err := actions.RegisterAction("DisposisiSurat", spy); err != nil {
		t.Fatalf("register action: %v", err)
	}
	rt := gatewaywf.Runtime{
		Engine: coreWf.New(defs, actions, coreWf.NewGuardEvaluator(),
			coreWf.WithTemplates(templates), coreWf.WithNotifier(notifierGagal{})),
		Instances: testkit.NewMemoryInstanceStore(),
	}
	h := gatewaywf.NewHandler(stubProvider{rt: rt})

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances",
		`{"slot":"`+slotUji+`","entity_id":"`+uuid.New().String()+`"}`, coreWf.PermInstanceMulai))
	instID, _ := decodeBody(t, w)["id"].(string)

	w = serve(t, h, req(t, http.MethodPost, "/workflow/instances/"+instID+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum"}}`,
		coreWf.PermInstanceTransisi))
	if w.Code == http.StatusOK {
		t.Fatalf("kegagalan notifikasi disembunyikan; status %d", w.Code)
	}

	// Meski responsnya error, transisinya SUDAH terjadi dan harus terbaca dari store.
	w = serve(t, h, req(t, http.MethodGet, "/workflow/instances/"+instID, "", coreWf.PermInstanceBaca))
	if state := decodeBody(t, w)["current_state"]; state != "didisposisi" {
		t.Fatalf("current_state = %v, ingin didisposisi (transisi hilang meski action sudah jalan)", state)
	}
}

// Entitas yang alurnya sudah berjalan/selesai tak boleh dimulai ulang. Tanpa pagar ini, siapa pun
// yang boleh memulai alur bisa membuat instance baru di initial_state untuk surat yang sudah
// ditutup, lalu menjalankan lagi seluruh action-nya — mendisposisi ulang, berkali-kali, tanpa satu
// pun gerbang yang dilanggar.
func TestHandler_StartKeduaUntukEntitasSamaDitolak(t *testing.T) {
	spy := &actionSpy{}
	h := gatewaywf.NewHandler(stubProvider{rt: newRuntime(t, spy)})
	entityID := uuid.New()
	body := `{"slot":"` + slotUji + `","entity_id":"` + entityID.String() + `"}`

	if w := serve(t, h, req(t, http.MethodPost, "/workflow/instances", body,
		coreWf.PermInstanceMulai)); w.Code != http.StatusCreated {
		t.Fatalf("start pertama: %d — %s", w.Code, w.Body.String())
	}

	w := serve(t, h, req(t, http.MethodPost, "/workflow/instances", body, coreWf.PermInstanceMulai))

	if w.Code != http.StatusConflict {
		t.Fatalf("start kedua: status %d, ingin 409 (%s)", w.Code, w.Body.String())
	}
}
