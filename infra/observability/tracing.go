// tracing.go menyiapkan distributed tracing OTEL: TracerProvider yang
// mengekspor span ke collector via OTLP/gRPC. Tidak ada port domain untuk
// tracing (beda dengan MetricsPort) karena propagasi span berjalan lewat
// context.Context standar Go — use case/adapter cukup panggil
// observability.Tracer(name) dan tak perlu injeksi interface baru
// (PRD F3: span gateway -> use case -> adapter).
package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingOptions mengatur TracerProvider. Diisi dari config.ObservabilityConfig
// saat wiring (di cmd/server) — adapter ini tak mengimport core/config, sama
// seperti LogOptions, agar tetap decoupled.
type TracingOptions struct {
	Enabled     bool   // dari GOV_TRACING_ENABLED
	Endpoint    string // dari GOV_OTEL_ENDPOINT, host:port collector OTLP gRPC
	ServiceName string // nama service untuk resource attribute service.name
}

// NewTracerProvider membuat & mendaftarkan (otel.SetTracerProvider) TracerProvider
// global. Bila Enabled=false, provider tetap valid (span tetap terbentuk untuk
// pemanggil) tapi tanpa exporter — span di-drop, bukan diekspor — sehingga kode
// pemanggil tidak perlu percabangan aktif/nonaktif.
//
// Pemanggil bertanggung jawab men-Shutdown provider yang dikembalikan saat
// server berhenti (flush batch span tertunda).
func NewTracerProvider(ctx context.Context, opts TracingOptions) (*sdktrace.TracerProvider, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceNameKey.String(serviceNameOrDefault(opts.ServiceName)),
	))
	if err != nil {
		return nil, fmt.Errorf("observability: buat resource: %w", err)
	}

	if !opts.Enabled {
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return tp, nil
	}

	// GOV_OTEL_ENDPOINT ditulis dengan skema (mis. "http://otel-collector:4317") di
	// contoh CLAUDE.md, tapi otlptracegrpc.WithEndpoint mau host:port polos.
	endpoint := strings.TrimPrefix(strings.TrimPrefix(opts.Endpoint, "https://"), "http://")
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // koneksi ke collector di jaringan internal (bukan lintas internet)
	)
	if err != nil {
		return nil, fmt.Errorf("observability: buat OTLP exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp, nil
}

func serviceNameOrDefault(name string) string {
	if name == "" {
		return "pamong"
	}
	return name
}

// Tracer mengembalikan trace.Tracer bernama name dari TracerProvider global
// (yang didaftarkan NewTracerProvider). Span mengalir lewat context.Context
// standar: gateway membuka span request, use case & adapter melanjutkannya
// lewat ctx yang sama — tidak ada port baru untuk ini.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
