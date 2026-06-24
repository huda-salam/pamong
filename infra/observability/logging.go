// Package observability berisi driven adapter untuk logging, metrics, dan tracing.
// Logging memakai log/slog stdlib: JSON terstruktur, level dari config, dan
// correlation ID otomatis dari context (port.CorrelationID).
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/huda-salam/pamong/port"
)

// LogOptions mengatur logger. Diisi dari config.ObservabilityConfig saat wiring
// (di-map di cmd/server) — adapter ini tidak mengimport core/config agar tetap decoupled.
type LogOptions struct {
	Level  string    // debug | info | warn | error (default: info)
	Format string    // json | text (default: json)
	Output io.Writer // default: os.Stdout
}

// slogLogger mengimplementasi port.Logger di atas *slog.Logger.
type slogLogger struct {
	l *slog.Logger
}

var _ port.Logger = (*slogLogger)(nil)

// NewLogger membuat port.Logger berbasis slog sesuai opsi.
func NewLogger(opts LogOptions) port.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	handlerOpts := &slog.HandlerOptions{Level: parseLevel(opts.Level)}

	var h slog.Handler
	switch opts.Format {
	case "text":
		h = slog.NewTextHandler(out, handlerOpts)
	default: // json sebagai default aman untuk produksi
		h = slog.NewJSONHandler(out, handlerOpts)
	}
	return &slogLogger{l: slog.New(h)}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// log adalah inti: menambahkan correlation ID dari context lalu meneruskan ke slog.
func (s *slogLogger) log(ctx context.Context, level slog.Level, msg string, fields []port.Field) {
	attrs := make([]any, 0, len(fields)+1)
	if cid := port.CorrelationID(ctx); cid != "" {
		attrs = append(attrs, slog.String("correlation_id", cid))
	}
	for _, f := range fields {
		attrs = append(attrs, slog.Any(f.Key, f.Value))
	}
	s.l.Log(ctx, level, msg, attrs...)
}

func (s *slogLogger) Debug(ctx context.Context, msg string, fields ...port.Field) {
	s.log(ctx, slog.LevelDebug, msg, fields)
}
func (s *slogLogger) Info(ctx context.Context, msg string, fields ...port.Field) {
	s.log(ctx, slog.LevelInfo, msg, fields)
}
func (s *slogLogger) Warn(ctx context.Context, msg string, fields ...port.Field) {
	s.log(ctx, slog.LevelWarn, msg, fields)
}
func (s *slogLogger) Error(ctx context.Context, msg string, fields ...port.Field) {
	s.log(ctx, slog.LevelError, msg, fields)
}

// With mengembalikan logger turunan dengan fields permanen.
func (s *slogLogger) With(fields ...port.Field) port.Logger {
	attrs := make([]any, 0, len(fields))
	for _, f := range fields {
		attrs = append(attrs, slog.Any(f.Key, f.Value))
	}
	return &slogLogger{l: s.l.With(attrs...)}
}
