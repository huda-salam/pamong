package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// JobStore adalah driven port persistensi jadwal & riwayat. Diimplementasi di
// infra/scheduler (Postgres) dan oleh MemoryJobStore (test). Core tak tahu detail DB.
//
// Kontrak konkurensi (single-instance untuk PR-3.5.1): DueSchedules hanya membaca.
// Anti double-run di multi-instance ditegakkan lewat lock terdistribusi di PR-3.5.2 —
// port ini sengaja tak mengklaim baris agar penambahan lock tak mengubah signature.
type JobStore interface {
	// SaveSchedule menyimpan (upsert by ID) satu jadwal.
	SaveSchedule(ctx context.Context, job ScheduledJob) error

	// GetSchedule mengambil satu jadwal by ID (ErrNotFound bila tak ada).
	GetSchedule(ctx context.Context, id uuid.UUID) (ScheduledJob, error)

	// DueSchedules mengembalikan jadwal aktif yang NextRunAt <= now, terurut NextRunAt.
	DueSchedules(ctx context.Context, now time.Time) ([]ScheduledJob, error)

	// UpdateAfterRun memperbarui LastRunAt/NextRunAt (dan Enabled untuk one-shot) setelah
	// eksekusi. nextRun zero + enabled=false menandai one-shot yang sudah selesai.
	UpdateAfterRun(ctx context.Context, id uuid.UUID, lastRun time.Time, nextRun time.Time, enabled bool) error

	// RecordRun menyimpan satu baris riwayat eksekusi.
	RecordRun(ctx context.Context, run JobRun) error

	// GetRun mengambil satu baris riwayat by ID — dipakai Replay (ErrNotFound bila tak ada).
	GetRun(ctx context.Context, id uuid.UUID) (JobRun, error)

	// Runs mengembalikan riwayat untuk satu jadwal, terbaru dulu, dibatasi limit.
	Runs(ctx context.Context, scheduleID uuid.UUID, limit int) ([]JobRun, error)
}
