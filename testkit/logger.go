package testkit

import (
	"context"
	"sync"

	"github.com/huda-salam/pamong/port"
)

// NoopLogger adalah port.Logger yang membuang semua log. Dipakai saat use case butuh
// logger tapi test tidak memverifikasi output log.
type NoopLogger struct{}

var _ port.Logger = (*NoopLogger)(nil)

func NewNoopLogger() *NoopLogger { return &NoopLogger{} }

func (l *NoopLogger) Debug(context.Context, string, ...port.Field) {}
func (l *NoopLogger) Info(context.Context, string, ...port.Field)  {}
func (l *NoopLogger) Warn(context.Context, string, ...port.Field)  {}
func (l *NoopLogger) Error(context.Context, string, ...port.Field) {}
func (l *NoopLogger) With(...port.Field) port.Logger               { return l }

// CapturingLogger merekam log untuk diassert. Berguna saat test ingin memastikan
// suatu kejadian dicatat (mis. error path).
type CapturingLogger struct {
	mu      sync.Mutex
	Entries []LogEntry
}

// LogEntry adalah satu baris log yang terekam.
type LogEntry struct {
	Level   string
	Message string
	Fields  []port.Field
}

var _ port.Logger = (*CapturingLogger)(nil)

func NewCapturingLogger() *CapturingLogger { return &CapturingLogger{} }

func (l *CapturingLogger) record(level, msg string, fields []port.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries = append(l.Entries, LogEntry{Level: level, Message: msg, Fields: fields})
}

func (l *CapturingLogger) Debug(_ context.Context, msg string, f ...port.Field) {
	l.record("debug", msg, f)
}
func (l *CapturingLogger) Info(_ context.Context, msg string, f ...port.Field) {
	l.record("info", msg, f)
}
func (l *CapturingLogger) Warn(_ context.Context, msg string, f ...port.Field) {
	l.record("warn", msg, f)
}
func (l *CapturingLogger) Error(_ context.Context, msg string, f ...port.Field) {
	l.record("error", msg, f)
}
func (l *CapturingLogger) With(...port.Field) port.Logger { return l }
