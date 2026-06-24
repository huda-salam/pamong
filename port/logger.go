package port

import "context"

// Field adalah pasangan key-value terstruktur untuk log. Pakai F() untuk membuatnya.
type Field struct {
	Key   string
	Value any
}

// F adalah konstruktor ringkas Field: port.F("surat_id", id).
func F(key string, value any) Field { return Field{Key: key, Value: value} }

// Logger adalah port logging terstruktur. Domain & use case menerima Logger lewat
// injeksi, tidak pernah membuat logger global sendiri. Implementasi (slog) ada di
// infra/observability. Setiap method menerima context agar correlation ID ikut tercatat.
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...Field)
	Info(ctx context.Context, msg string, fields ...Field)
	Warn(ctx context.Context, msg string, fields ...Field)
	Error(ctx context.Context, msg string, fields ...Field)

	// With mengembalikan Logger turunan yang selalu menyertakan fields tambahan.
	With(fields ...Field) Logger
}

type correlationKey struct{}

// WithCorrelationID menyisipkan correlation ID ke context. Middleware gateway
// memanggilnya di awal request; logger membacanya otomatis di setiap log line.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID mengambil correlation ID dari context, atau "" bila tidak ada.
func CorrelationID(ctx context.Context) string {
	if v, ok := ctx.Value(correlationKey{}).(string); ok {
		return v
	}
	return ""
}
