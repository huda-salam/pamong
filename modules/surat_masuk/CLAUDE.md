# modules/surat_masuk — Modul Referensi

Modul referensi resmi Pamong. Mendemonstrasikan SEMUA konvensi pada kasus nyata
(persuratan: surat masuk + disposisi berjenjang). Saat membuat modul baru, tiru pola
di sini. Bukan modul mainan — ini standar yang harus diikuti.

## Apa yang didemonstrasikan
- Struktur hexagonal lengkap (domain/usecase/adapter + manifest + bootstrap)
- Entity Tier 3 (SuratMasuk) dengan Audit & Lockable eksplisit
- Use case dengan permission check di baris pertama, validasi, event publish
- Workflow-as-data (disposisi.yaml) — action memanggil use case, bukan logic
- Dependency ke modul lain lewat port (UserResolver) — bukan import langsung
- Event produce/consume terdaftar di manifest, pakai konstanta
- Repository port di domain, implementasi di adapter/db
- Migration up/down dengan kolom standar
- Unit test: happy path + permission denied + validasi gagal

## Bergantung pada
- port/ (repository, eventbus, auth, user, sequence, workflow)
- Tidak import internal modul lain; data user lewat UserResolver port

## Peta file (urut baca)
1. manifest.go        — identitas, entity, event, permission, workflow, dependency
2. domain/entity.go   — SuratMasuk, Disposisi (EntityDef + domain struct)
3. domain/ports.go    — Repository + dependency port (UserResolver dipakai)
4. domain/events.go   — konstanta event
5. domain/errors.go   — domain errors
6. usecase/create_surat_masuk.go  — buat surat (Tier 3 use case)
7. usecase/disposisi_surat.go     — disposisi (dipanggil workflow action)
8. adapter/http/handler.go        — handler tipis (parse/delegate/respond)
9. adapter/db/repository.go       — implementasi Repository (Postgres)
10. adapter/event/consumer.go     — consume event modul lain (contoh)
11. workflows/disposisi.yaml      — definisi workflow (seed)
12. migrations/001_*.sql          — DDL up/down
13. bootstrap.go      — wiring DI (satu-satunya tempat bind port->adapter)

## Pola kunci untuk ditiru
- Use case: RequirePermission DULUAN, lalu validasi, lalu logic (lewat port), lalu
  persist, lalu publish event.
- Handler: TIDAK ada logic; hanya bind input, panggil use case, tulis response.
- Workflow action "disposisi" = nama use case DisposisiSurat; engine memanggilnya.
- Uang? Tidak ada di modul ini, tapi bila ada gunakan decimal.Decimal.

## Test
- go test ./modules/surat_masuk/... -race
- Wajib: happy path, permission-denied, validasi gagal per use case.

## Rujukan
- ../../docs/CODE_CONVENTION.md, ../../docs/CODING_PHILOSOPHY.md
- ../../core/workflow/PRD.md (use case vs workflow)
