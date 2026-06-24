# infra/observability — Metrics, Tracing, Logging

Driven adapter: implementasi port.MetricsPort + structured logging + tracing OTEL.
Prometheus-compatible metrics, distributed tracing, JSON logs dengan correlation id.

## Bergantung pada
- port/metrics.go; OTEL, Prometheus client

## Tanggung jawab
- MetricsPort: counter, gauge, histogram (per modul/tenant tags)
- Structured logging (JSON, correlation id, level dari config)
- Distributed tracing (OTEL) -> collector (Jaeger/Tempo)

## File kunci
- metrics.go — MetricsPort impl (Prometheus)
- logging.go — structured logger
- tracing.go — OTEL setup

## Konvensi khusus
- Semua log JSON dengan correlation id dari gateway request id.
- Metrics tags: module, tenant, endpoint (hindari high-cardinality berlebihan).

## Test
- Unit: metric recording, log format.
- go test ./infra/observability/...

## Rujukan
- PRD.md, port/metrics.go, CODING_PHILOSOPHY.md (observability)
