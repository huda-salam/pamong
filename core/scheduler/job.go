package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// JobFunc adalah handler job — kode Go ter-compile & ter-test yang didaftarkan ke Registry.
// payload adalah argumen job (opaque, biasanya JSON) yang tersimpan di schedule; job WAJIB
// idempoten bila memungkinkan agar replay & at-least-once aman (PRD scheduler, anti-pattern).
type JobFunc func(ctx context.Context, payload []byte) error

// RunStatus adalah status satu eksekusi job.
type RunStatus string

const (
	StatusSuccess RunStatus = "success"
	StatusFailed  RunStatus = "failed"
)

// ScheduledJob adalah satu entri jadwal tersimpan. CronExpr menentukan sifatnya:
//   - CronExpr != "" → berulang: setelah jalan, NextRunAt dihitung ulang dari cron.
//   - CronExpr == "" → sekali-jalan (one-shot): fires sekali di NextRunAt lalu Enabled=false.
//     Bentuk one-shot inilah yang dipakai deadline scheduling SLA workflow (PRD F2) —
//     tak perlu mekanisme terpisah, cukup daftarkan job one-shot pada waktu deadline.
//
// Yang tersimpan di DB hanya JobKey (rujukan handler ter-registry) + Payload + jadwal —
// bukan logika. TenantID mengikat job ke satu tenant (kosong = job level-platform).
type ScheduledJob struct {
	ID        uuid.UUID
	TenantID  string
	Name      string // nama deskriptif jadwal (mis. "rekonsiliasi-harian")
	JobKey    string // key handler di Registry
	CronExpr  string // ekspresi cron; kosong = one-shot
	Payload   []byte
	Enabled   bool
	NextRunAt time.Time
	LastRunAt *time.Time
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

// IsOneShot melaporkan apakah job berjalan sekali saja (tanpa cron berulang).
func (j ScheduledJob) IsOneShot() bool { return j.CronExpr == "" }

// JobRun adalah satu baris riwayat eksekusi. Payload di-snapshot di sini agar replay
// (PRD F4) berjalan "dengan konteks yang sama" tanpa bergantung pada schedule yang
// mungkin sudah berubah/terhapus. ScheduleID nil untuk job ad-hoc (Trigger langsung).
type JobRun struct {
	ID         uuid.UUID
	ScheduleID *uuid.UUID
	TenantID   string
	JobKey     string
	Payload    []byte
	Status     RunStatus
	StartedAt  time.Time
	FinishedAt time.Time
	Error      string
	Attempt    int
}
