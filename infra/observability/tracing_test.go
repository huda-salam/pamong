package observability_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/huda-salam/pamong/infra/observability"
)

// fakeCollector adalah implementasi minimal TraceServiceServer (protokol OTLP
// yang sama dipakai collector sungguhan) yang merekam setiap request Export
// yang diterima — dipakai membuktikan span benar-benar terkirim lewat gRPC,
// tanpa bergantung pada collector eksternal/Docker saat unit test.
type fakeCollector struct {
	coltracepb.UnimplementedTraceServiceServer
	received chan *coltracepb.ExportTraceServiceRequest
}

func (f *fakeCollector) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	f.received <- req
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// startFakeCollector menjalankan server gRPC OTLP palsu di port ephemeral
// localhost, dan mengembalikan alamatnya + fungsi cleanup.
func startFakeCollector(t *testing.T) (addr string, received chan *coltracepb.ExportTraceServiceRequest) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	col := &fakeCollector{received: make(chan *coltracepb.ExportTraceServiceRequest, 1)}
	srv := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(srv, col)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), col.received
}

// TestNewTracerProvider_SpanMuncul_DiCollector memenuhi DoD PRD PR-3.7.2:
// trace muncul di collector. "Collector" di sini adalah server gRPC OTLP
// palsu (protokol identik dengan collector sungguhan) sehingga test
// deterministik tanpa Docker; jalur produksi memakai Endpoint dari
// GOV_OTEL_ENDPOINT ke collector nyata (unchanged).
func TestNewTracerProvider_SpanMuncul_DiCollector(t *testing.T) {
	addr, received := startFakeCollector(t)

	ctx := context.Background()
	tp, err := observability.NewTracerProvider(ctx, observability.TracingOptions{
		Enabled:     true,
		Endpoint:    addr,
		ServiceName: "pamong-test",
	})
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer("test").Start(ctx, "operasi_uji")
	span.End()

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	select {
	case req := <-received:
		found := false
		for _, rs := range req.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, sp := range ss.GetSpans() {
					if sp.GetName() == "operasi_uji" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("collector menerima request tapi span 'operasi_uji' tidak ditemukan: %+v", req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout menunggu span diterima collector")
	}
}

// TestNewTracerProvider_Disabled_TidakError memastikan Enabled=false tetap
// menghasilkan provider valid (span terbentuk, tak ada exporter) tanpa error —
// use case/adapter tidak perlu percabangan aktif/nonaktif tracing.
func TestNewTracerProvider_Disabled_TidakError(t *testing.T) {
	tp, err := observability.NewTracerProvider(context.Background(), observability.TracingOptions{Enabled: false})
	if err != nil {
		t.Fatalf("NewTracerProvider(disabled): %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Error("span harus tetap valid meski tracing disabled (hanya tak diekspor)")
	}
}

// TestTracer_PakaiGlobalProvider membuktikan observability.Tracer(name) memakai
// TracerProvider global yang didaftarkan NewTracerProvider.
func TestTracer_PakaiGlobalProvider(t *testing.T) {
	tp, err := observability.NewTracerProvider(context.Background(), observability.TracingOptions{Enabled: false, ServiceName: "svc"})
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := observability.Tracer("modul_x").Start(context.Background(), "op")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Error("span dari observability.Tracer() harus valid")
	}
}
