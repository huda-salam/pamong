# PRD: Event Bus

## Tujuan
Menyediakan messaging async antar modul (dan kelak antar service) dengan guaranteed
delivery, schema validation, dan ketahanan (retry/DLQ). Memungkinkan modular monolith
berkomunikasi tanpa coupling langsung, dan menjadi tulang punggung sync identity.

## Kebutuhan fungsional
- F1: Publish/Subscribe lewat driver (memory/NATS/Redis Streams) dipilih config.
- F2: Schema registry: event terdaftar (dari manifest); publish event tanpa schema /
  payload tak sesuai → tolak.
- F3: Outbox pattern: event ditulis ke tabel outbox dalam transaksi bisnis yang sama;
  relay mengirim setelah commit. Rollback transaksi → event tidak terkirim.
- F4: Retry dengan backoff; gagal melebihi batas → DLQ + alert.
- F5: Idempoten di sisi consumer didukung (event membawa idempotency key bila perlu).

## Kebutuhan non-fungsional
- Publish (via outbox) tidak menambah latensi transaksi signifikan.
- At-least-once delivery; consumer harus idempoten.
- Driver memory untuk test cepat tanpa infra.

## Dependency
- port/eventbus.go; infra/db (outbox table); pustaka nats/redis.

## Anti-pattern
- Publish langsung tanpa outbox (event hilang saat crash). Event tanpa schema.

## Acceptance criteria
- [ ] Publish/subscribe lokal (memory) lulus; event tanpa schema ditolak.
- [ ] Outbox: rollback transaksi → event tidak terkirim.
- [ ] NATS/Redis: publish-subscribe lintas proses (integration).
- [ ] Handler gagal → masuk DLQ setelah N retry.
