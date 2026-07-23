package scheduler

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
)

// MemoryJobStore adalah implementasi JobStore in-memory untuk unit test & dev single-instance.
// Bukan untuk produksi multi-instance (tak ada lock lintas proses) — gunakan infra/scheduler.
type MemoryJobStore struct {
	mu        sync.Mutex
	schedules map[uuid.UUID]ScheduledJob
	runs      []JobRun
}

var _ JobStore = (*MemoryJobStore)(nil)

// NewMemoryJobStore membuat store kosong.
func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{schedules: make(map[uuid.UUID]ScheduledJob)}
}

func (s *MemoryJobStore) SaveSchedule(_ context.Context, job ScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules[job.ID] = job
	return nil
}

func (s *MemoryJobStore) GetSchedule(_ context.Context, id uuid.UUID) (ScheduledJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.schedules[id]
	if !ok {
		return ScheduledJob{}, core.ErrNotFound("scheduled job", id.String())
	}
	return j, nil
}

func (s *MemoryJobStore) DueSchedules(_ context.Context, now time.Time) ([]ScheduledJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []ScheduledJob
	for _, j := range s.schedules {
		if j.Enabled && !j.NextRunAt.After(now) {
			due = append(due, j)
		}
	}
	sort.Slice(due, func(i, k int) bool { return due[i].NextRunAt.Before(due[k].NextRunAt) })
	return due, nil
}

func (s *MemoryJobStore) UpdateAfterRun(_ context.Context, id uuid.UUID, lastRun, nextRun time.Time, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.schedules[id]
	if !ok {
		return core.ErrNotFound("scheduled job", id.String())
	}
	last := lastRun
	j.LastRunAt = &last
	j.NextRunAt = nextRun
	j.Enabled = enabled
	s.schedules[id] = j
	return nil
}

func (s *MemoryJobStore) RecordRun(_ context.Context, run JobRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, run)
	return nil
}

func (s *MemoryJobStore) GetRun(_ context.Context, id uuid.UUID) (JobRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return JobRun{}, core.ErrNotFound("job run", id.String())
}

func (s *MemoryJobStore) Runs(_ context.Context, scheduleID uuid.UUID, limit int) ([]JobRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []JobRun
	for _, r := range s.runs {
		if r.ScheduleID != nil && *r.ScheduleID == scheduleID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].StartedAt.After(out[k].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
