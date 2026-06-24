# PRD: Custom Linter (Go Analyzer)

## Tujuan
Menegakkan konvensi Pamong yang tidak bisa ditangkap compiler (Lapis 3 enforcement).
Setiap rule berakar pada CODE_CONVENTION/CODING_PHILOSOPHY dan ditegakkan di editor + CI.
Tujuannya: pelanggaran konvensi gagal lebih awal, bukan ditemukan saat review/produksi.

## Konteks & batasan
### Jadi tanggung jawab
- Analyzer custom berbasis golang.org/x/tools/go/analysis
- Satu package per rule, masing-masing dengan testdata (analysistest)
- Registry yang menggabungkan semua rule, dijalankan via `pamongctl lint` / multichecker
### BUKAN tanggung jawab
- Aturan yang sudah dicakup gofmt/go vet/staticcheck (jangan duplikasi)
- Validasi runtime (itu boot-time checks)

## Daftar rule (kontrak per rule)

| Rule | Mendeteksi | Akar |
|---|---|---|
| domain-no-infra-import | import infra/adapter/eksternal dari domain/ atau usecase/ | hexagonal |
| handler-must-check-permission | handler/usecase tanpa RequirePermission di awal | keamanan |
| handler-no-direct-repo | handler mengakses repository langsung | hexagonal |
| event-must-use-const | publish event dengan string literal | eksplisit |
| permission-must-be-registered | pakai permission modul lain tanpa Imports di manifest | boundary |
| entity-explicit-auditable | EntityDef tanpa Audit/Lockable eksplisit | eksplisit |
| raw-sql-must-annotate | raw SQL tanpa komentar gov:raw-ok + alasan | auditabilitas |
| config-no-direct-env | os.Getenv di dalam modul | config terpusat |
| migration-needs-down | file migration up tanpa pasangan down | reversibility |
| no-cross-module-import | import package internal modul lain | modular monolith |
| no-cross-schema-join | JOIN lintas-schema modul lain dalam query | modular monolith |
| workflow-action-no-logic | business logic / akses DB di action workflow | use case vs workflow |
| tenant-branch-must-be-strategy | if tenant.x untuk pilihan algoritma | strategy registry |
| strategy-key-must-be-registered | strategy key dirujuk tanpa terdaftar | open/closed |

## Struktur implementasi (referensi: rules/domainnoinfra)
```
tools/linter/
├── registry.go                 # gabungkan semua Analyzer, entry multichecker
└── rules/{rule}/
    ├── analyzer.go             # *analysis.Analyzer + Run
    ├── analyzer_test.go        # analysistest.Run(t, testdata, Analyzer)
    └── testdata/src/...        # contoh clean (tanpa diagnostik) & dirty (// want "...")
```

## Kebutuhan non-fungsional
- Tiap rule punya testdata clean (tidak memicu) & dirty (memicu, diuji dengan `// want`).
- Analyzer cepat; dapat dijalankan inkremental oleh editor (gopls) & di CI.
- Pesan diagnostik jelas + menyebut cara perbaikan / rujukan konvensi.

## Anti-pattern
- Rule false-positive tinggi (mengganggu) — utamakan presisi, sediakan anotasi escape
  yang terdokumentasi (mis. gov:raw-ok) bila perlu.
- Menduplikasi go vet/staticcheck.

## Acceptance criteria
- [ ] Setiap rule punya testdata clean & dirty; `go test ./tools/linter/...` lulus.
- [ ] domain-no-infra-import: import infra dari domain → diagnostik; clean → tidak.
- [ ] Registry menjalankan semua rule via satu entry (multichecker).
- [ ] `pamongctl lint` mengembalikan exit non-zero saat ada pelanggaran.
