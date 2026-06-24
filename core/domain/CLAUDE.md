# core/domain — Domain Engine

Jantung framework. Mengelola registry modul, definisi entity, lifecycle hooks, dan
sistem tier (Tier 1/2/3). Dari definisi entity di sini, framework auto-generate tabel,
endpoint CRUD, permission default, audit, dan fiscal check. Komponen yang membuat
"convention over configuration" jadi nyata.

## Bergantung pada
- `port/` (repository, fiscal, eventbus)
- Standard library Go saja

## Tidak boleh
- Import infra/, adapter/, gateway/, identity/, modules/ [linter: domain-no-infra-import]
- Menyimpan state global mutable di luar registry yang dikelola eksplisit

## Tanggung jawab
- Module registry: register, discovery, validasi dependency sebagai DAG
- Entity definition: EntityDef, FieldDef, tipe field, validasi struktural
- Manifest: parsing, validasi, ekstraksi (events, permissions, workflows, config)
- Lifecycle hooks: before/after save, submit, delete — dipanggil framework, bukan modul
- Tier resolution: Tier 1 (YAML->runtime CRUD), Tier 2 (+hooks), Tier 3 (+hexagonal)
- Generation pipeline: dari EntityDef -> DDL, endpoint, permission, audit attachment

## BUKAN tanggung jawab
- Eksekusi SQL (itu infra/db) — domain hanya mendefinisikan bentuk
- HTTP routing (itu gateway) — domain hanya mendeklarasikan endpoint yang dibutuhkan
- Logika bisnis modul (itu usecase tiap modul)

## File kunci
- registry.go — global registry, Register(), discovery, DAG resolver
- manifest.go — interface Module, struct Manifest, validasi
- entity.go — EntityDef, FieldDef, validasi
- field_types.go — enum tipe field + aturan validasi per tipe
- hook.go — HookSet, hook runner, urutan eksekusi
- tier.go — resolusi tier, kontrak Tier 1 runtime
- generation.go — orkestrasi auto-generate dari EntityDef

## Konvensi khusus
- EntityDef WAJIB deklarasikan Audit & Lockable eksplisit — tipe tanpa zero value valid
  agar lupa-mengisi = gagal kompilasi/boot, bukan diam-diam salah.
- Registry satu-satunya sumber kebenaran modul terdaftar. Jangan instantiate engine
  modul di luar registry.

## Pitfall umum
- Default diam-diam pada field keputusan (audit, lockable) — dilarang.
- Hook dengan side-effect DB langsung — hook memanggil use case/port.
- Mengasumsikan urutan Register() = urutan dependency. Urutan ditentukan DAG resolver
  dari DependsOn.

## Test
- Unit: validasi tiap tipe field, DAG cycle detection, hook ordering.
- Integration: Tier 1 entity (YAML only) -> endpoint CRUD berfungsi.
- go test ./core/domain/... -race

## Rujukan
- PRD.md, port/repository.go, port/fiscal.go, modules/surat_masuk/
