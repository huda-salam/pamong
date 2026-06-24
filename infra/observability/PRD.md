# PRD: Observability

## Tujuan
Memberi visibilitas operasional: metrics (Prometheus), tracing (OTEL), dan structured
logging dengan correlation id, agar masalah produksi dapat didiagnosis dan SLA dipantau
per tenant/modul.

## Kebutuhan fungsional
- F1: MetricsPort: RecordDuration (histogram), IncrCounter, SetGauge, dengan tags
  (module, tenant, endpoint). Endpoint Prometheus untuk scrape.
- F2: Structured logging JSON dengan correlation id (dari request id gateway), level
  dari config.
- F3: Distributed tracing OTEL; span dari gateway → use case → adapter; export ke collector.

## Kebutuhan non-fungsational
- Overhead instrumentation minimal (< 1ms per operasi umum).
- Hindari label high-cardinality (mis. id entitas) pada metrics.

## Dependency
- port/metrics.go; OTEL & Prometheus client; config (endpoint, level).

## Anti-pattern
- Log tak terstruktur / tanpa correlation id. Metrics dengan label high-cardinality.
- Double logging (log + return error yang sama di banyak layer).

## Acceptance criteria
- [ ] Metric ter-expose di endpoint Prometheus dengan tags benar.
- [ ] Log keluar JSON dengan correlation id.
- [ ] Trace muncul di collector dengan span lintas layer.
