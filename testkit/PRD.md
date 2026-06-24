# PRD: testkit — Testing Utilities

## Tujuan
Menyediakan mock, fixture, dan helper agar modul dapat diuji tanpa infrastruktur nyata
(unit) dan dengan infrastruktur efemeral (integration), serta menjaga contract test antar
modul. Mendukung CODING_PHILOSOPHY #9: test sebagai bukti pengaman bekerja.

## Konteks & batasan
### Jadi tanggung jawab
- Mock port (repository, publisher, metrics, dll) untuk unit test
- Builder AuthContext untuk skenario permission/persona
- Helper DB efemeral (testcontainers) untuk integration test
- Helper contract test event schema
### BUKAN tanggung jawab
- Test bisnis spesifik modul (ditulis di modul)

## Komponen & kontrak
```go
// Mock port
testkit.NewMockRepo[T]()              // BaseRepository in-memory
testkit.NewMockPublisher()            // EventPublisher; merekam event untuk asersi
testkit.NewMockMetrics()              // MetricsPort no-op + perekam
testkit.NewMockFiscalChecker(status)  // FiscalChecker yang mengembalikan status tertentu

// AuthContext builder
testkit.NewContext(t,
    testkit.WithPersona("employee"),
    testkit.WithTenant("pemkot-a"),
    testkit.WithRole("bendahara"),
    testkit.WithPermission("penatausahaan:spm:buat"),
)

// Integration
testkit.NewTestDB(t)                  // Postgres efemeral (testcontainers), migrasi siap
testkit.Seed[T](t, db, entity)        // seed data

// Contract test
testkit.AssertEventSchema(t, name, payload)   // payload sesuai schema terdaftar
testkit.AssertEventPublished(pub, name)        // event tertentu terbit
```

## Kebutuhan non-fungsional
- Unit test berjalan tanpa jaringan/DB, cepat (< ms per test).
- Integration helper mengelola lifecycle container (start/stop) otomatis per test/package.
- Mock mengikuti kontrak port persis (compile-time assertion).

## Anti-pattern
- Mock yang menyimpang dari kontrak port (menyembunyikan bug integrasi).
- Helper integration yang membuat test flaky (lifecycle container tak dikelola).

## Acceptance criteria
- [ ] MockRepo/Publisher/Metrics memenuhi interface port (compile-time assertion).
- [ ] NewContext membangun AuthContext dengan role/persona/permission sesuai opsi.
- [ ] NewTestDB menyediakan Postgres efemeral dengan migrasi terpasang.
- [ ] AssertEventSchema gagal saat payload tak sesuai schema.
