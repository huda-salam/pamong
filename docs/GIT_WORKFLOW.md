# GIT_WORKFLOW.md — Panduan Git & Workflow Pengembangan Pamong

Panduan ini mengatur bagaimana perubahan kode masuk ke repo: mulai dari
membuat branch, menulis commit, sampai PR di-merge. Aturan di sini
berpasangan dengan `CODING_PHILOSOPHY.md` dan `CLAUDE.md`.

---

## Ringkasan cepat

```
1. Buat branch dari staging (bukan main)
2. Satu PR = satu job dari ROADMAP
3. Commit dengan format konvensional
4. Semua gate CI harus hijau sebelum minta review
5. Squash-merge ke staging; staging → main hanya via release
```

---

## Branch

### Struktur

```
main          ← production-ready; hanya menerima merge dari staging via release
  └── staging ← integration; menerima merge dari feat/fix/refactor
        ├── feat/...
        ├── fix/...
        ├── refactor/...
        ├── chore/...
        └── docs/...
```

- **`main`** di-protect: tidak ada push langsung, tidak ada merge tanpa CI hijau.
- **`staging`** di-protect: tidak ada push langsung; butuh ≥ 1 approval.
- Semua branch kerja dibuat dari `staging`, bukan dari `main`.

### Penamaan branch

Format: `{type}/{deskripsi-singkat-kebab-case}`

| Type | Kapan dipakai |
|---|---|
| `feat` | Fitur baru, implementasi job ROADMAP |
| `fix` | Bugfix |
| `refactor` | Perubahan internal tanpa perubahan behavior |
| `chore` | Dependency update, config, tooling |
| `docs` | Dokumentasi, ADR |
| `sec` | Patch keamanan |

Contoh:
```
feat/pr-0.1.1-bootstrap-monorepo
feat/pr-1.1.1-manifest-module-registry
fix/workflow-guard-nil-actor
refactor/eventbus-outbox-goroutine
docs/adr-004-multi-tenant-schema
chore/upgrade-pgx-v5.10
```

### Lifecycle branch

```bash
# Mulai kerja
git checkout staging
git pull origin staging
git checkout -b feat/pr-X.Y.Z-deskripsi

# Selama pengerjaan — rebase, bukan merge, agar history bersih
git fetch origin staging
git rebase origin/staging

# Selesai — push dan buat PR
git push -u origin feat/pr-X.Y.Z-deskripsi
```

**Jangan merge `staging` ke dalam branch kerja** — selalu `rebase`. Ini menjaga
history linear dan memudahkan `git bisect`.

---

## Commit

### Format

```
{type}({scope}): {deskripsi lowercase, imperatif}
```

Contoh:
```
feat(core/domain): tambah interface Module dan App container
feat(surat_masuk): implementasi use case create dengan permission check
fix(workflow): guard expression gagal evaluate nil actor
refactor(eventbus): pisah outbox writer ke goroutine terpisah
test(surat_masuk): tambah contract test event schema
docs(adr): ADR-004 keputusan multi-tenant schema strategy
chore(deps): upgrade pgx v5.10.0
perf(keuangan): optimasi query laporan realisasi dengan materialized view
sec(auth): perbaiki validasi JTI saat token revocation
```

### Type valid

`feat` | `fix` | `refactor` | `test` | `docs` | `chore` | `perf` | `sec`

Gunakan `sec` untuk patch keamanan agar mudah di-audit di changelog.

### Scope

Nama package atau komponen yang berubah:
- Framework core: `core/domain`, `core/workflow`, `core/rules`, `port`, `gateway`
- Infra: `infra/db`, `infra/storage`, `infra/eventbus`
- Modul bisnis: `surat_masuk`, `keuangan`, `kepegawaian`
- Tooling: `pamongctl`, `linter`, `testkit`
- Identity: `identity`

### Aturan commit

- **Satu commit = satu unit logis.** Jangan campur refactor dengan fitur baru.
- **Deskripsi imperatif**, lowercase: "tambah", "perbaiki", "hapus" — bukan "menambahkan".
- **Tidak ada trailing period** di baris pertama.
- Body commit (opsional) untuk menjelaskan *mengapa*, bukan *apa*:
  ```
  feat(core/domain): tambah WorkflowRegistry ke App

  WorkflowRegistry dipisah dari Router agar modul bisa mendaftarkan
  action tanpa bergantung pada package gateway. Ini menjaga core/domain
  bebas dari import net/http (linter: domain-no-infra-import).
  ```
- **Jangan commit kode yang tidak dikompilasi.** Jalankan `go build ./...` sebelum commit.
- Tidak ada `TODO`/`FIXME` tanpa referensi GitHub issue (`// TODO: #123 ...`).

---

## Pull Request

### Satu PR = satu job ROADMAP

Setiap PR mengimplementasi tepat satu job dari `ROADMAP.md`. Ini membuat
PR reviewable dalam satu sesi dan dependency antar job eksplisit.

Pengecualian: hotfix boleh tanpa job ROADMAP, tapi harus kecil (< 100 baris inti).

### Checklist sebelum minta review

Semua harus terpenuhi — CI akan menolak jika tidak:

```
[ ] Branch up-to-date dengan staging (git rebase, bukan merge)
[ ] go build ./... lulus
[ ] go test -race ./... lulus
[ ] go vet ./... bersih
[ ] pamongctl lint ./... bersih (bila pamongctl sudah tersedia)
[ ] Coverage tidak turun dari baseline (domain ≥ 90%, usecase ≥ 85%)
[ ] Tidak ada TODO/FIXME tanpa issue GitHub terkait
[ ] Migration baru punya down migration
[ ] Event schema baru punya contract test
[ ] Permission baru terdaftar di manifest
[ ] Perubahan core punya ADR baru atau ADR yang di-update
[ ] go mod tidy sudah dijalankan
```

### Template deskripsi PR

```markdown
## Apa yang berubah
<!-- 2-3 kalimat: masalah apa, solusi apa. -->

## Job ROADMAP
<!-- Misal: PR-0.1.1 — Inisialisasi monorepo -->

## Tipe perubahan
- [ ] feat — fitur baru
- [ ] fix — bugfix
- [ ] refactor — perubahan internal tanpa perubahan behavior
- [ ] breaking change — perubahan yang memerlukan update di modul lain

## Modul / package yang terpengaruh
<!-- Daftar package yang berubah atau perlu diupdate -->

## Cara test manual
<!-- Langkah yang bisa diikuti reviewer untuk verifikasi -->

## Checklist
- [ ] Unit test untuk kode baru
- [ ] Integration test (jika menyentuh adapter)
- [ ] Migration punya down migration
- [ ] Event schema baru di-contract test
- [ ] ADR dibuat/diupdate (jika perubahan arsitektur)

## Referensi
<!-- Issue, ADR, atau diskusi yang relevan -->
```

### Aturan review

| Jenis perubahan | Approver minimum |
|---|---|
| `modules/` — modul bisnis | 1 maintainer modul terkait |
| `core/` — framework core | 1 maintainer core |
| `identity/` | 1 maintainer core (data sentral, sensitif) |
| `infra/`, `gateway/` | 1 maintainer core |
| `docs/`, `chore/` | 1 siapapun |

- **Auto-merge dilarang** untuk semua branch.
- Reviewer tidak boleh approve PR miliknya sendiri.
- Jika reviewer request changes, author wajib merespons setiap komentar sebelum
  re-request review — tidak boleh silent dismiss.

### Merge strategy

- **feat/fix/refactor → staging**: **Squash merge**. Semua commit dalam branch
  di-squash menjadi satu commit bersih di staging. Pesan commit squash mengikuti
  format konvensional di atas.
- **staging → main**: **Merge commit** (bukan squash), dilakukan saat release.
  Ini mempertahankan titik merge sebagai penanda rilis yang jelas.

---

## Tagging & rilis

```
v{major}.{minor}.{patch}
v0.1.0   ← Phase 0 selesai
v0.2.0   ← Phase 1 selesai
v1.0.0   ← Phase 7 selesai (framework fungsional penuh)
```

- Tag selalu di `main`, bukan di `staging`.
- Setiap tag punya release notes yang menyebut job ROADMAP yang tercakup.
- Pre-release (alpha/beta) menggunakan suffix: `v0.3.0-alpha.1`.

---

## Troubleshooting umum

### Rebase konflik

```bash
git rebase origin/staging
# Selesaikan konflik di setiap file
git add .
git rebase --continue
```

Jika rebase terlalu kompleks (banyak konflik bertumpuk), diskusikan dulu dengan
tim sebelum force-push — ada kemungkinan PR perlu dipecah.

### CI merah setelah rebase

Jalankan ulang secara lokal:
```bash
make test
make vet
```

Jangan push dengan `--no-verify` atau skip CI — temukan akar masalahnya.

### Salah branch dasar (branch dari main, bukan staging)

```bash
# Pindah base branch ke staging tanpa kehilangan commit
git rebase --onto staging main feat/nama-branch
```

---

## CI/CD (Gitea Actions)

Pamong memakai **Gitea Actions** sebagai CI — self-hosted, gratis, dan syntax-nya
kompatibel dengan GitHub Actions (migrasi ke GitHub di masa depan tinggal pindah
direktori workflow). `CLAUDE.md §20` mendefinisikan *gate apa* yang harus lulus;
seksi ini mendefinisikan *bagaimana* gate itu dijalankan di Gitea.

### Lokasi file workflow

```
.gitea/workflows/*.yaml      ← dibaca Gitea Actions (WAJIB di sini)
```

Gitea juga membaca `.github/workflows/` sebagai fallback, tapi untuk repo ini
**selalu pakai `.gitea/workflows/`** agar eksplisit. Satu file per pipeline:

| File | Tujuan | Trigger |
|---|---|---|
| `ci.yaml` | lint → test → build per push/PR | push & PR ke `staging`, `main` |

### Konvensi penulisan workflow

- **`runs-on: ubuntu-latest`** — runner self-hosted memetakan label ini ke image
  `docker.gitea.com/runner-images:ubuntu-latest` (full-featured: git, curl, node).
- **Job dipecah per tahap** (`lint`, `test`, `build`) agar kegagalan mudah dibaca,
  bukan satu job monolitik.
- **Versi Go di-pin** (`go-version: "1.26"`), tidak pakai `stable`/`latest` —
  konsisten dengan aturan pin versi di `CODE_CONVENTION.md §8`.
- **Gate yang belum bisa ditegakkan** (pamongctl, coverage, integration test) ditulis
  sebagai komentar `# TODO: PR-X.Y.Z ...` di workflow — bukan dihapus. Ini menjaga
  peta gate masa depan tetap terlihat dan terlacak ke ROADMAP (lihat
  `CODE_CONVENTION.md §9`).

### Pemetaan gate ROADMAP → step CI

Gate diaktifkan bertahap seiring tooling-nya selesai. Jangan aktifkan step yang
tool-nya belum ada — CI harus selalu hijau untuk kode yang benar.

| Gate (CLAUDE.md §20) | Diaktifkan di | Status |
|---|---|---|
| gofmt, go vet, go mod tidy | PR-0.1.2 | ✅ aktif |
| `go test -race` | PR-0.1.2 | ✅ aktif |
| `go build` | PR-0.1.2 | ✅ aktif |
| `pamongctl lint` | PR-0.3.2 | TODO di workflow |
| Coverage per-layer | PR-0.3.3 | TODO di workflow |
| Integration test + Postgres | PR-1.2.1 | TODO di workflow |
| `pamongctl validate modules` | PR-1.1.1 | TODO di workflow |
| `pamongctl audit deps` (CVE) | PR-5.x | belum |

### Branch protection

Konfigurasi di Gitea (**Settings → Branches → Branch protection**), bukan di repo:

- `main` & `staging`: wajib status check CI hijau sebelum merge.
- `main` & `staging`: wajib ≥ 1 approval (lihat tabel review di atas).
- Disable push langsung; semua lewat PR.

### Menjalankan ulang & debugging CI

- Lihat hasil run di Gitea: tab **Actions** pada repo.
- Sebelum push, reproduksi gate secara lokal:
  ```bash
  gofmt -l .
  go vet ./...
  go test -race -count=1 ./...
  go build ./...
  ```
- **Jangan** push dengan `--no-verify` atau menonaktifkan CI untuk "mengejar" merge.

### Setup runner (referensi operasional)

Runner adalah container `gitea/act_runner` pada `docker-compose` yang sama dengan
Gitea, satu network, dengan `/var/run/docker.sock` ter-mount (agar bisa menjalankan
service container & testcontainers untuk integration test). Registrasi sekali via
token dari **Site Administration → Runners**. Detail langkah ada di catatan
operasional infrastruktur, di luar repo aplikasi.

---

## Referensi silang

- `CLAUDE.md` §18 — konvensi branch & commit (ringkasan)
- `CLAUDE.md` §19 — template PR
- `CLAUDE.md` §20 — CI/CD gates (definisi konseptual)
- `.gitea/workflows/ci.yaml` — implementasi pipeline
- `ROADMAP.md` — daftar job dan dependency antar job
- `docs/CODING_PHILOSOPHY.md` — mengapa aturan ini ada
