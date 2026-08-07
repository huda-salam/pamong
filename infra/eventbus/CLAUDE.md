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
- **Driver memory hanya untuk dev/test — kini DITEGAKKAN config.** `AppConfig.Validate` menolak
  `eventbus.driver` memory ATAU KOSONG di luar development (`""` dipetakan `newDriver` ke driver
  yang sama). Alasannya bukan sekadar durability: memory mengantar SINKRON dan `errors.Join`
  error handler kembali ke pemanggil `Publish`, jadi satu subscriber yang gagal menggagalkan use
  case SESUDAH mutasi bisnisnya commit — dan percobaan ulang menabrak invariant anti-duplikat
  yang dibuat percobaan pertama.
- **Ada subscriber = wajib `Bus.Drain()` saat shutdown**, sesudah server berhenti menerima
  request dan SEBELUM pool DB ditutup. Driver tanpa `Drainer` = no-op. `NATSDriver.Drain`
  MENUNGGU sampai koneksi tertutup (batas 10 dtk): `nats.Conn.Drain()` sendiri kembali seketika,
  jadi meneruskannya apa adanya membuat pemanggil menutup pool di bawah kaki handler yang
  masih berjalan.
- **Payload tak boleh memuat nilai kelas `personal_id`** (ADR-009 §6, ADR-018).
  `gov.outbox_events.payload` adalah JSONB plaintext dan stream NATS punya retensi, jadi pengenal
  yang lewat sini terbaca dari dump. Ditutup dengan MENGHAPUS nilainya (consumer meresolusi lewat
  port di sisi pemilik data), bukan menyegelnya: blob yang mengendap di stream/outbox menjadi
  kewajiban dekripsi permanen yang melintasi rotasi kunci dan patahan format ciphertext.
- **Registry schema diisi dari MANIFEST, bukan daftar tangan di composition root** (PR-5.1.5).
  `domain.Registry.RegisterEventSchemas(bus.Schema())` mendaftarkan `Events.Produces` semua modul;
  dipanggil `cmd/server` sesudah `registry.Validate()` dan SEBELUM Bootstrap (titik pertama modul
  bisa menerbitkan event). Komponen non-modul (identity, customization) tetap punya registrar
  sendiri karena tak ber-manifest. Nama event yang diklaim dua modul dengan tipe payload berbeda
  = gagal boot.
- **Tak ada konsep versi schema.** `SchemaRegistry` mencocokkan identitas TIPE GO, dan `Unmarshal`
  memakai encoding/json yang mengabaikan key tak dikenal. Menambah ATAU menghapus field karena itu
  backward-tolerant di kawat; yang ditolak hanya tipe payload yang berbeda untuk nama event yang
  sama. Checklist PR "naikkan versi schema" belum punya mesin di belakangnya.

## Pitfall umum
- Publish tanpa schema terdaftar [linter: event-must-use-const di sisi pemanggil].
- Kehilangan event saat crash (gunakan outbox, bukan publish langsung).
- **Mendaftarkan subscription NATS tanpa Flush.** `nc.Subscribe` hanya menaruh protokol SUB
  di buffer tulis klien; NATS Core MEMBUANG pesan yang tiba sebelum server mencatatnya, tanpa
  re-delivery. Akibatnya event yang di-dispatch pada jendela antara Bootstrap dan pencatatan
  SUB hilang permanen padahal OutboxRelay sudah menandainya terkirim. `NATSDriver.Subscribe`
  karena itu blokir sampai server mengonfirmasi — jangan dilepas demi "boot lebih cepat".
  Gejalanya dulu muncul sebagai test yang tampak flaky, bukan sebagai error.

## Test
- Unit: schema validation, retry/DLQ (memory driver).
- Integration: NATS/Redis publish-subscribe lintas proses; outbox rollback.
- go test ./infra/eventbus/... (unit) / -tags=integration

## Rujukan
- PRD.md, port/eventbus.go
