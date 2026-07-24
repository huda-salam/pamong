package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
)

// Driven adapter SLA workflow (PR-3.2.6): menjembatani port core/workflow ke core/scheduler
// (penjadwalan deadline) dan core/notification (pengiriman eskalasi). Sengaja DI LUAR core —
// core/workflow tak pernah mengimport scheduler/notification secara konkret; ia hanya tahu
// port DeadlineScheduler & Escalator (arsitektur hexagonal, CLAUDE.md §Arsitektur).

// EscalationJobKey adalah key handler eskalasi di scheduler.Registry. Handler-nya (EscalationJob)
// membungkus EscalationCoordinator.Deliver — didaftarkan sekali saat bootstrap.
const EscalationJobKey = "workflow.sla.escalate"

// deadlineNS adalah namespace tetap untuk menurunkan ID job scheduler yang DETERMINISTIK dari
// Deadline.Key (UUIDv5). Deterministik agar penjadwalan idempoten dan pembatalan bisa menyasar
// job yang sama tanpa menyimpan mapping key→id.
var deadlineNS = uuid.MustParse("b7c0a3d2-9e41-5f68-8a12-3c4d5e6f7a80")

// jobIDForKey menurunkan ID job scheduler dari kunci deadline (stabil lintas panggilan).
func jobIDForKey(key string) uuid.UUID { return uuid.NewSHA1(deadlineNS, []byte(key)) }

// SchedulerDeadlines mengimplementasi coreWf.DeadlineScheduler di atas scheduler.Runner
// (menjadwalkan job one-shot pada waktu deadline) + scheduler.JobStore (membatalkan job).
// Handler EscalationJobKey WAJIB sudah terdaftar di registry Runner saat bootstrap.
type SchedulerDeadlines struct {
	runner *scheduler.Runner
	store  scheduler.JobStore
}

var _ coreWf.DeadlineScheduler = (*SchedulerDeadlines)(nil)

// NewSchedulerDeadlines merakit adapter. runner menjadwalkan (dan memvalidasi JobKey terdaftar);
// store dipakai membatalkan (menonaktifkan job) — keduanya berbagi penyimpanan jadwal yang sama.
func NewSchedulerDeadlines(runner *scheduler.Runner, store scheduler.JobStore) *SchedulerDeadlines {
	return &SchedulerDeadlines{runner: runner, store: store}
}

// ScheduleDeadline mendaftarkan satu job one-shot (CronExpr kosong) pada d.FireAt yang, saat
// dijalankan, memanggil EscalationJob → EscalationCoordinator.Deliver dengan payload Escalation.
func (s *SchedulerDeadlines) ScheduleDeadline(ctx context.Context, d coreWf.Deadline) error {
	payload, err := json.Marshal(d.Escalation)
	if err != nil {
		return fmt.Errorf("encode escalation: %w", err)
	}
	_, err = s.runner.Schedule(ctx, scheduler.ScheduledJob{
		ID:        jobIDForKey(d.Key),
		TenantID:  d.Escalation.TenantID,
		Name:      d.Key,
		JobKey:    EscalationJobKey,
		CronExpr:  "", // one-shot: fires sekali di NextRunAt lalu nonaktif
		Payload:   payload,
		Enabled:   true,
		NextRunAt: d.FireAt,
	})
	return err
}

// CancelDeadline menonaktifkan job deadline (idempoten): key yang tak pernah dijadwalkan atau
// sudah lewat/terhapus bukan error. Backstop kebenaran tetap guard race di EscalationCoordinator.
func (s *SchedulerDeadlines) CancelDeadline(ctx context.Context, key string) error {
	id := jobIDForKey(key)
	job, err := s.store.GetSchedule(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil // tak ada yang perlu dibatalkan
		}
		return err
	}
	job.Enabled = false
	return s.store.SaveSchedule(ctx, job)
}

// EscalationJob membungkus EscalationCoordinator sebagai scheduler.JobFunc: decode payload
// Escalation lalu Deliver (yang menjalankan guard race + eskalasi). Daftarkan ke registry
// dengan EscalationJobKey saat bootstrap.
func EscalationJob(coord *coreWf.EscalationCoordinator) scheduler.JobFunc {
	return func(ctx context.Context, payload []byte) error {
		var e coreWf.Escalation
		if err := json.Unmarshal(payload, &e); err != nil {
			return fmt.Errorf("decode escalation payload: %w", err)
		}
		return coord.Deliver(ctx, e)
	}
}

// RoleNotify adalah kontrak minimal yang dipakai NotifierEscalator — dipenuhi oleh
// *notification.RoleNotifier. Didefinisikan sebagai interface agar adapter teruji tanpa
// merakit seluruh Hub notifikasi.
type RoleNotify interface {
	NotifyRole(ctx context.Context, t notification.RoleTarget, templateKey string, data map[string]any, channels ...string) (int, error)
}

// NotifierEscalator mengimplementasi coreWf.Escalator di atas notification.RoleNotifier:
// memetakan Escalation → RoleTarget lalu mengirim template eskalasi ke channel yang diminta.
// Resolusi peran→orang (termasuk fallback PLT) terjadi di dalam RoleNotifier (core/notification).
//
// DEFERRED(PR-3.6.x): terapkan RoleBindings tenant pada Escalation.EscalateToRole SEBELUM
// merutekan — peran di Escalation masih generik (tenant-agnostik dari definisi). Binding
// diambil lewat TemplateStore.GetForTenant (BUKAN DefinitionStore.Get mentah) saat konsumsi
// role binding dinyalakan; titik penyisipannya persis di sini (lihat ROADMAP backlog 3.6.x).
type NotifierEscalator struct {
	notifier    RoleNotify
	templateKey string
	channels    []string
}

var _ coreWf.Escalator = (*NotifierEscalator)(nil)

// NewNotifierEscalator merakit escalator. templateKey = kunci template notifikasi eskalasi
// (di-resolve per-tenant+locale oleh hub); channels = channel tujuan (mis. "in_app").
func NewNotifierEscalator(notifier RoleNotify, templateKey string, channels ...string) *NotifierEscalator {
	return &NotifierEscalator{notifier: notifier, templateKey: templateKey, channels: channels}
}

// Escalate mengirim notifikasi eskalasi ke peran tujuan. Ini murni NOTIFIKASI — tak ada
// business logic / mutasi data (PRD F6).
func (n *NotifierEscalator) Escalate(ctx context.Context, e coreWf.Escalation) error {
	target := notification.RoleTarget{
		TenantID: e.TenantID,
		Role:     e.EscalateToRole,
	}
	_, err := n.notifier.NotifyRole(ctx, target, n.templateKey, map[string]any{
		"instance_id": e.InstanceID.String(),
		"state":       e.State,
		"role":        e.EscalateToRole,
	}, n.channels...)
	return err
}

// isNotFound melaporkan apakah err adalah core.ErrNotFound (untuk pembatalan idempoten).
func isNotFound(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "NOT_FOUND"
}
