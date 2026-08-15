// Package workflow adalah driving adapter HTTP untuk RUNTIME workflow (PR-W4a): memulai
// instance dari template yang dipilih tenant, menjalankan transisi, dan membaca state+riwayat.
//
// Ia hidup di gateway/ karena permukaan ini milik FRAMEWORK, bukan satu modul bisnis — sama
// seperti CRUD auto-generate Tier 1. Modul menyumbang definisi (YAML) dan action (use case);
// mesin dan endpoint-nya framework yang sediakan, sekali untuk semua modul.
//
// Tumpukan workflow per-tenant (engine + store instance) datang lewat seam RuntimeProvider yang
// dirakit composition root (ADR-022 Keputusan 3) — handler tak pernah menyentuh pool DB, dan
// karenanya tak pernah bisa keliru melayani tenant lain.
package workflow

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// Runtime adalah tumpukan workflow SATU tenant: engine (yang store definisi & template-nya sudah
// terikat ke DB tenant itu) beserta penyimpanan instance-nya.
type Runtime struct {
	Engine    *coreWf.Engine
	Instances coreWf.InstanceStore
}

// RuntimeProvider menyerahkan tumpukan workflow untuk satu tenant. Diimplementasi composition
// root (cmd/server) yang tahu cara membuka pool tenant; handler cukup tahu bahwa tumpukan yang
// diterimanya SUDAH milik tenant yang diminta.
type RuntimeProvider interface {
	RuntimeFor(ctx context.Context, tenantID string) (Runtime, error)
}

// Handler adalah driving adapter runtime workflow.
type Handler struct {
	runtimes RuntimeProvider
}

// NewHandler merakit handler. Provider wajib non-nil — rute yang terdaftar tapi menunjuk
// provider nil baru panic pada request pertama, di produksi.
func NewHandler(runtimes RuntimeProvider) *Handler {
	if runtimes == nil {
		panic("gateway/workflow: RuntimeProvider nil")
	}
	return &Handler{runtimes: runtimes}
}

// --- DTO kawat ---

type startRequest struct {
	Slot     string    `json:"slot"`      // tipe alur, mis. "surat_masuk.disposisi"
	EntityID uuid.UUID `json:"entity_id"` // entitas bisnis yang dikelola alur ini
}

// transitionRequest — Params adalah ARGUMEN action (niat aktor). Tidak ada field untuk snapshot
// entity: guard hanya boleh dievaluasi terhadap keadaan yang sudah tersimpan, tak pernah
// terhadap nilai yang dikirim aktor dalam request yang sama (ADR-022 Keputusan 2).
type transitionRequest struct {
	Action  string         `json:"action"`
	Params  map[string]any `json:"params"`
	Comment string         `json:"comment"`
}

type instanceResponse struct {
	ID                uuid.UUID          `json:"id"`
	DefinitionID      string             `json:"definition_id"`
	DefinitionVersion int                `json:"definition_version"`
	EntityID          uuid.UUID          `json:"entity_id"`
	CurrentState      string             `json:"current_state"`
	Version           int                `json:"version"`
	History           []transitionRecord `json:"history"`
}

type transitionRecord struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Action    string    `json:"action"`
	ActorID   uuid.UUID `json:"actor_id"`
	Timestamp string    `json:"timestamp"`
	Comment   string    `json:"comment"`
}

// --- Handler ---

// StartInstance menangani POST /workflow/instances: memulai alur dari template yang DIPILIH
// tenant untuk slot yang diminta (StartFromTemplate, bukan Start mentah — lihat ADR-012:
// Start tidak pernah menerapkan RoleBindings, sehingga notifikasi & eskalasi alur ini diam-diam
// tak akan sampai ke siapa pun).
func (h *Handler) StartInstance(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(coreWf.PermInstanceMulai); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in startRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Slot == "" {
		gateway.WriteError(w, core.ErrValidation("slot", "wajib diisi"))
		return
	}
	if in.EntityID == uuid.Nil {
		gateway.WriteError(w, core.ErrValidation("entity_id", "wajib diisi"))
		return
	}

	rt, err := h.runtime(ctx)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	inst, err := rt.Engine.StartFromTemplate(ctx, in.Slot, in.EntityID)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	// Simpan SEBELUM merespons: instance yang dilaporkan 201 tapi tak tersimpan adalah alur yang
	// dianggap berjalan oleh klien dan tak pernah ada di server.
	if err := rt.Instances.Save(ctx, inst); err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, toResponse(inst))
}

// Transition menangani POST /workflow/instances/{id}/transitions: satu transisi pada instance
// yang sudah berjalan.
//
// Urutan sengaja: baca instance → jalankan transisi (guard → action → state+history) → simpan.
// Engine memastikan transisi gagal tak mengubah instance, dan Save ber-optimistic-lock menolak
// hasil yang lahir dari state yang sudah basi.
func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(coreWf.PermInstanceTransisi); err != nil {
		gateway.WriteError(w, err)
		return
	}
	id, ok := instanceID(w, r)
	if !ok {
		return
	}
	var in transitionRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Action == "" {
		gateway.WriteError(w, core.ErrValidation("action", "wajib diisi"))
		return
	}

	rt, err := h.runtime(ctx)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	// KUNCI transisi SEBELUM instance dibaca dan action dijalankan.
	//
	// Optimistic locking pada Save saja TIDAK menutup ini, dan itu kekeliruan yang mudah dibuat:
	// guard versi memang menolak penulis yang kalah — tapi menolaknya SESUDAH action-nya terlanjur
	// berjalan (baris disposisi tertulis, event terbit). Yang terlindungi jadi hanya baris
	// instance, bukan efek bisnisnya: satu surat terdisposisi dua kali dengan satu jejak di
	// history. Guard versi tetap dipertahankan pada Save sebagai jaring kedua.
	//
	// Ditolak, bukan diantrekan: transisi bersamaan pada satu instance adalah tabrakan niat
	// (dua orang mendisposisi surat yang sama), bukan beban yang perlu diserialisasi.
	release, locked, err := rt.Instances.TryLockInstance(ctx, id)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	defer release()
	if !locked {
		gateway.WriteError(w, core.ErrConflict(
			"transisi lain atas instance ini sedang berjalan; coba lagi"))
		return
	}

	inst, err := h.load(ctx, rt, id)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}

	// Entity snapshot untuk guard belum tersedia dari sini: membacanya menuntut seam
	// "resolver snapshot entity" per tipe entity, yang belum ada dan BUKAN lingkup W4a.
	//
	// Nil di sini BUKAN "guard dievaluasi terhadap kekosongan": evaluator menolak PEMBACAAN
	// `entity.x` bila snapshot tak disediakan sama sekali (ADR-022 Keputusan 7), jadi transisinya
	// gagal alih-alih lolos. Itu perlu ditegakkan di evaluator, bukan diasumsikan di sini —
	// sebelum ADR-022, `entity.status != 'dibatalkan'` bernilai TRUE terhadap nil.
	// Guard ber-actor (yang dipakai definisi baseline repo ini) tak terpengaruh.
	// DEFERRED(PR-W4c): resolver snapshot entity + otorisasi tingkat entitas.
	stateSebelum, riwayatSebelum := inst.CurrentState, len(inst.History)
	execErr := rt.Engine.ExecuteRequest(ctx, inst, coreWf.TransitionRequest{
		Action:  in.Action,
		Params:  in.Params,
		Comment: in.Comment,
	})
	// Transisi yang SUDAH terjadi wajib tersimpan meskipun Execute mengembalikan error. Engine
	// membatalkan transisi untuk kegagalan guard/action (state tak berubah — tak ada yang perlu
	// disimpan), TAPI langkah sesudah transisi otoritatif (penjadwalan SLA, notifikasi) sengaja
	// mempropagasi error TANPA membatalkannya. Membuang instance di jalur itu berarti kehilangan
	// transisi yang efek bisnisnya sudah terjadi, dan membiarkan action-nya dijalankan ulang.
	// Kini dormant (deadlines & notifier belum dipasang, PR-W4b) — ditutup sekarang justru karena
	// saat W4b memasangnya, tak ada yang akan gagal untuk mengingatkan.
	berubah := inst.CurrentState != stateSebelum || len(inst.History) != riwayatSebelum
	if execErr != nil && !berubah {
		gateway.WriteError(w, execErr)
		return
	}
	if err := rt.Instances.Save(ctx, inst); err != nil {
		gateway.WriteError(w, err)
		return
	}
	if execErr != nil {
		gateway.WriteError(w, execErr)
		return
	}
	gateway.WriteJSON(w, http.StatusOK, toResponse(inst))
}

// GetInstance menangani GET /workflow/instances/{id}: state terkini + riwayat transisi.
func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(coreWf.PermInstanceBaca); err != nil {
		gateway.WriteError(w, err)
		return
	}
	id, ok := instanceID(w, r)
	if !ok {
		return
	}
	rt, err := h.runtime(ctx)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	inst, err := h.load(ctx, rt, id)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusOK, toResponse(inst))
}

// runtime mengambil tumpukan workflow milik tenant AKTOR. Tenant selalu dari klaim token
// (ctx.TenantID()), tak pernah dari body/query — permukaan ini karenanya selalu bekerja pada
// tenant si aktor.
func (h *Handler) runtime(ctx *gateway.Context) (Runtime, error) {
	tenantID := ctx.TenantID()
	if tenantID == "" {
		return Runtime{}, core.ErrUnauthorized("token tanpa tenant — pilih tenant lebih dulu")
	}
	return h.runtimes.RuntimeFor(port.WithTenant(ctx, tenantID), tenantID)
}

// load membaca instance dan menegaskan kepemilikannya oleh tenant aktor.
//
// Pemeriksaan tenant di sini adalah pertahanan BERLAPIS, bukan pagar utama: store instance sudah
// terikat pada DB tenant si aktor, jadi instance tenant lain memang tak terjangkau. Ia menangkap
// kelas kegagalan lain — pool yang salah di-route, atau baris yang tersalin antar DB saat naik
// tier — dan menjawabnya sebagai "tidak ada" alih-alih membocorkan bahwa ID itu memang ada.
func (h *Handler) load(ctx *gateway.Context, rt Runtime, id uuid.UUID) (*coreWf.WorkflowInstance, error) {
	inst, err := rt.Instances.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if inst.TenantID != ctx.TenantID() {
		return nil, coreWf.ErrInstanceNotFound(id.String())
	}
	return inst, nil
}

// instanceID membaca path param {id} sebagai UUID.
func instanceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		gateway.WriteError(w, core.ErrValidation("id", "bukan UUID yang valid"))
		return uuid.Nil, false
	}
	return id, true
}

// decode membaca body JSON dengan batas ukuran, memetakan kegagalan ke 400.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(dst); err != nil {
		gateway.WriteError(w, gateway.ErrBadRequest("body tidak valid"))
		return false
	}
	return true
}

// toResponse memetakan instance domain ke bentuk kawat. RoleBindings sengaja TIDAK dikembalikan:
// ia adalah konfigurasi tenant (peran generik → role konkret), bukan bagian dari status alur yang
// perlu diketahui klien.
func toResponse(inst *coreWf.WorkflowInstance) instanceResponse {
	history := make([]transitionRecord, 0, len(inst.History))
	for _, rec := range inst.History {
		history = append(history, transitionRecord{
			From:      rec.From,
			To:        rec.To,
			Action:    rec.Action,
			ActorID:   rec.ActorID,
			Timestamp: rec.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Comment:   rec.Comment,
		})
	}
	return instanceResponse{
		ID:                inst.ID,
		DefinitionID:      inst.DefinitionID,
		DefinitionVersion: inst.DefinitionVersion,
		EntityID:          inst.EntityID,
		CurrentState:      inst.CurrentState,
		Version:           inst.Version,
		History:           history,
	}
}

// MountRoutes memasang grup /workflow/* pada router BISNIS — di balik stack lengkap
// (Auth → RequireAuth → TenantResolver → RateLimit → Idempotency). Semuanya menuntut token
// ber-tenant, dan dua di antaranya memutasi state alur sehingga Idempotency-Key layak dihormati.
func MountRoutes(r port.Router, h *Handler) {
	r.Post("/workflow/instances", h.StartInstance)
	r.Get("/workflow/instances/{id}", h.GetInstance)
	r.Post("/workflow/instances/{id}/transitions", h.Transition)
}
