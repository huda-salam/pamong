package workflow

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SLA & eskalasi (PRD F6). Model batas waktu per-state: saat instance MASUK sebuah state
// ber-SLA, engine mendaftarkan satu deadline one-shot; saat instance KELUAR, engine
// membatalkannya. Bila deadline lewat sebelum dibatalkan, EscalationCoordinator menjalankan
// GUARD RACE (cek instance masih di state itu) lalu memicu notifikasi eskalasi.
//
// Semua ketergantungan ke luar (scheduler, notification, storage instance) lewat PORT yang
// didefinisikan di sini — engine tidak pernah menyentuh core/scheduler atau core/notification
// secara konkret. Engine tetap TENANT-AGNOSTIK: ia hanya bicara (instance, state, PERAN);
// resolusi peran→orang (binding tenant + fallback PLT) terjadi di adapter Escalator di luar.
//
// Eskalasi = NOTIFIKASI, bukan business logic: tidak ada mutasi data / pemanggilan use case
// di jalur ini.

// Escalation adalah muatan satu eskalasi: cukup untuk (a) guard race di fire-time dan
// (b) merutekan notifikasi ke peran yang tepat. EscalateToRole adalah PERAN sebagaimana
// tertulis di definisi (tenant-agnostik); pemetaan peran→role konkret tenant (RoleBindings)
// dan role→orang (PLT) dilakukan adapter Escalator, bukan di sini.
type Escalation struct {
	TenantID       string    // tenant pemilik instance (dari AuthContext saat penjadwalan)
	InstanceID     uuid.UUID // instance yang di-SLA-kan — kunci guard race
	State          string    // state ber-SLA; eskalasi hanya sah bila instance MASIH di sini
	EscalateToRole string    // peran tujuan notifikasi (dari State.EscalateToRole)
}

// Deadline adalah satu batas waktu SLA yang dijadwalkan. Key unik per (instance, state)
// sehingga penjadwalan idempoten dan pembatalan bisa menyasar tepat satu timer.
type Deadline struct {
	Key        string    // unik per (instance, state) — lihat DeadlineKey
	FireAt     time.Time // kapan deadline dianggap lewat (now + SLAHours)
	Escalation Escalation
}

// DeadlineKey membentuk kunci kanonik per (instance, state). Deterministik agar penjadwalan
// dan pembatalan merujuk timer yang sama tanpa perlu menyimpan ID di instance.
func DeadlineKey(instanceID uuid.UUID, state string) string {
	return "workflow.sla." + instanceID.String() + "." + state
}

// DeadlineScheduler adalah driven port penjadwalan deadline SLA. Diimplementasi DI LUAR core
// (adapter di infra/workflow atas core/scheduler, memakai job one-shot). Engine memanggil
// ScheduleDeadline saat masuk state ber-SLA dan CancelDeadline saat keluar.
//
// CancelDeadline WAJIB idempoten: membatalkan key yang tak ada / sudah lewat bukan error —
// backstop kebenaran tetap guard race di EscalationCoordinator.
type DeadlineScheduler interface {
	ScheduleDeadline(ctx context.Context, d Deadline) error
	CancelDeadline(ctx context.Context, key string) error
}

// InstanceStateReader membaca state terkini satu instance — dipakai HANYA untuk guard race
// saat deadline lewat (balapan transisi-vs-deadline). Diimplementasi di luar core (adapter
// atas penyimpanan instance). Mengembalikan core.ErrNotFound bila instance tak ada.
type InstanceStateReader interface {
	CurrentState(ctx context.Context, instanceID uuid.UUID) (string, error)
}

// Escalator mengirim notifikasi eskalasi setelah guard race lolos. Diimplementasi di luar
// core (adapter atas core/notification): resolusi peran→orang (binding tenant + fallback PLT)
// + render template + kirim channel terjadi DI SANA. Escalator TIDAK menjalankan business
// logic atau memutasi data — murni notifikasi (PRD F6, CLAUDE.md §7).
type Escalator interface {
	Escalate(ctx context.Context, e Escalation) error
}

// EscalationCoordinator adalah kebijakan inti fire-time: GUARD RACE dulu, baru eskalasi.
// Ia dibungkus oleh handler scheduler (JobFunc ter-registry) di layer wiring; logikanya
// hidup di core agar teruji & deterministik, sumber datanya (state reader, escalator) pluggable.
type EscalationCoordinator struct {
	instances InstanceStateReader
	escalator Escalator
}

// NewEscalationCoordinator merakit coordinator. Kedua dependency wajib non-nil.
func NewEscalationCoordinator(instances InstanceStateReader, escalator Escalator) *EscalationCoordinator {
	return &EscalationCoordinator{instances: instances, escalator: escalator}
}

// Deliver dijalankan saat deadline lewat. GUARD RACE: bila instance sudah PINDAH dari state
// yang di-SLA-kan (transisi mendahului deadline), ini no-op — deadline basi, tak ada eskalasi.
// Bila masih di state itu, picu Escalator. Kesalahan baca state / kirim dipropagasi agar
// scheduler mencatat run gagal (bisa di-replay).
func (c *EscalationCoordinator) Deliver(ctx context.Context, e Escalation) error {
	state, err := c.instances.CurrentState(ctx, e.InstanceID)
	if err != nil {
		return err
	}
	if state != e.State {
		return nil // instance sudah pindah — deadline basi, jangan eskalasi
	}
	return c.escalator.Escalate(ctx, e)
}
