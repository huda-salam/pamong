package workflow

import (
	"sync"

	"github.com/huda-salam/pamong/port"
)

// ActionRegistry adalah jembatan antara SISI DAFTAR (modul saat Bootstrap memanggil
// app.Workflow().RegisterAction) dan SISI PANGGIL (engine memanggil ActionDispatcher.Dispatch).
// Sebelum ADR-022 kedua sisi itu tak pernah bertemu: registry lama menampung `any` dan tak ada
// satu pun pembaca.
//
// Ia memenuhi dua kontrak sekaligus — domain.WorkflowRegistry (secara struktural; paket ini
// sengaja tidak mengimpor core/domain) dan ActionDispatcher — sehingga composition root cukup
// merakit SATU objek dan menyerahkannya ke App maupun ke setiap Engine per-tenant.
//
// Registry ini PROSES-LEBAR, bukan per-tenant: yang disimpan adalah kode modul (sama untuk semua
// tenant), bukan data tenant. Isolasi tenant hidup di lapisan bawahnya (repo & pool yang dipakai
// use case), bukan di pemetaan nama→action.
type ActionRegistry struct {
	mu      sync.RWMutex
	actions map[string]port.WorkflowAction
}

// NewActionRegistry membuat registry kosong.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{actions: make(map[string]port.WorkflowAction)}
}

var _ ActionDispatcher = (*ActionRegistry)(nil)

// RegisterAction mendaftarkan action ber-nama. Nama = nilai field `action` di definisi workflow
// (mis. "DisposisiSurat" di disposisi.yaml).
//
// Menolak action nil dan pendaftaran ganda — keduanya salah-wiring yang harus menjatuhkan boot.
// Pendaftaran ganda khususnya: dua modul yang memakai nama action sama akan saling menimpa diam-
// diam, dan yang menang ditentukan urutan Bootstrap (yaitu urutan DAG dependency) — transisi
// tenant lalu memanggil use case modul yang salah.
func (r *ActionRegistry) RegisterAction(name string, action port.WorkflowAction) error {
	if name == "" {
		return ErrInvalidAction("nama action kosong")
	}
	if action == nil {
		return ErrInvalidAction("action nil untuk " + name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.actions[name]; dup {
		return ErrActionDuplicate(name)
	}
	r.actions[name] = action
	return nil
}

// Names mengembalikan nama action terdaftar (untuk introspeksi & log boot). Tidak terurut.
func (r *ActionRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.actions))
	for n := range r.actions {
		names = append(names, n)
	}
	return names
}

// Dispatch memanggil action terdaftar. Nama tak dikenal → ErrActionUnknown, yang membuat engine
// membatalkan transisi (state tidak berubah) alih-alih memindahkan state tanpa efek bisnis apa
// pun — definisi yang menyebut action tak terdaftar adalah bug wiring, bukan alur yang sah.
func (r *ActionRegistry) Dispatch(ctx port.AuthContext, action string, instance WorkflowInstance, params map[string]any) error {
	r.mu.RLock()
	a, ok := r.actions[action]
	r.mu.RUnlock()
	if !ok {
		return ErrActionUnknown(action)
	}
	return a.RunWorkflowAction(ctx, port.WorkflowActionInput{
		TenantID:   instance.TenantID,
		InstanceID: instance.ID,
		EntityID:   instance.EntityID,
		Action:     action,
		Params:     params,
	})
}
