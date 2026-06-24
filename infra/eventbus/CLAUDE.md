# infra/eventbus — Event Bus Drivers

Driven adapter: implementasi port.EventPublisher + EventSubscriber. Driver memory
(testing), NATS, Redis Streams. Outbox pattern untuk guaranteed delivery, DLQ + retry.

## Bergantung pada
- port/eventbus.go; pustaka driver (nats/redis)

## Tanggung jawab
- Publish/subscribe lewat driver yang dipilih config
- Schema registry & validasi event (event tanpa schema -> tolak)
- Outbox: event tersimpan transaksional, dikirim setelah commit
- DLQ + retry backoff

## File kunci
- bus.go — entry, dispatch ke driver
- outbox.go — outbox writer & relay
- schema.go — registry & validasi event
- retry.go, dlq.go — retry policy, dead letter
- drivers/ — memory.go, nats.go, redis.go

## Konvensi khusus
- Event name terdaftar di manifest modul; schema divalidasi saat publish.
- Outbox: tulis event ke tabel outbox dalam transaksi bisnis; relay async.
- Driver memory hanya untuk test.

## Pitfall umum
- Publish tanpa schema terdaftar [linter: event-must-use-const di sisi pemanggil].
- Kehilangan event saat crash (gunakan outbox, bukan publish langsung).

## Test
- Unit: schema validation, retry/DLQ (memory driver).
- Integration: NATS/Redis publish-subscribe lintas proses; outbox rollback.
- go test ./infra/eventbus/... (unit) / -tags=integration

## Rujukan
- PRD.md, port/eventbus.go
