# ADR-024: Efek samping transisi workflow — niat yang ditulis, bukan aksi yang dijalankan; SLA level-triggered

## Status

Proposed

Amends ADR-023 (bagian ALASAN "anggaran koneksi", bukan keputusan residensinya).
Melengkapi ADR-012 (bridge workflow→notifikasi) dan ADR-022 (kontrak dispatch action).

## Konteks

PR-W4b memasang runtime SLA + notifikasi yang hidup. Perakitannya menuntut **dua putaran
`/code-review` dengan sembilan temuan**, dan **tiga temuan putaran-2 adalah konsekuensi
perbaikan putaran-1**. Itu bukan kesialan; itu gejala satu cacat struktural yang selama ini
tak terlihat karena komponennya tak pernah dirakit.

### Cacat struktural: efek samping dijalankan sebelum baris otoritatifnya tersimpan

Urutan yang berlaku sekarang pada satu transisi HTTP:

```
core/workflow/engine.go ExecuteRequest
  1. guard → dispatch action → mutasi instance DI MEMORI (state + history)
  2. cancelSLA / scheduleSLA   → TULIS ke DB SENTRAL   (gov.scheduled_jobs)   :265-270
  3. notifier.NotifyTransition → TULIS ke DB TENANT + KIRIM email             :277-282
  4. return err
gateway/workflow/handler.go Transition
  5. Instances.Save(ctx, inst) → TULIS ke DB TENANT (baris instance)          :222
  6. lapor execErr                                                            :226
```

Langkah 2 dan 3 adalah **efek samping yang durable dan sebagian tak bisa dibatalkan**, dan
keduanya terjadi **sebelum** langkah 5 menyimpan satu-satunya baris yang membuat transisi itu
nyata. Tidak ada transaksi yang melingkupi 2–5, dan tak mungkin ada: langkah 2 menulis ke
database yang berbeda dari langkah 5. Ini *dual-write* klasik.

Konsekuensi konkretnya, bukan teoretis:

- **Gagal di langkah 5 setelah langkah 3 berhasil** → penerima sudah menerima email "surat
  selesai" untuk transisi yang tidak jadi tersimpan. Email tak bisa di-rollback.
- **Gagal di langkah 5 setelah langkah 2 berhasil** → ada baris deadline di DB sentral untuk
  instance yang tak pernah ada. Saat jatuh tempo, guard race gagal membaca instance, job
  ditandai gagal, lalu dibuang diam-diam (lihat M3 di bawah).
- **`Engine.Start` / `StartFromTemplate` punya cacat yang sama** (`engine.go:88`, `:139`): `scheduleSLA` untuk initial state
  berjalan sebelum `StartInstance` menyimpan instance (`handler.go:126`).

### Kenapa satu `error` tak bisa menyelesaikannya

`ExecuteRequest` mengembalikan satu `error` untuk tiga kelas hasil yang berbeda secara
fundamental:

| Kelas | Sudah terjadi? | Retry aman? |
|---|---|---|
| Transisi ditolak (guard/action gagal) | tidak | ya, benar |
| Transisi terjadi, gagal disimpan | sebagian | tidak |
| Transisi terjadi & tersimpan, efek samping gagal | ya | **berbahaya** |

Go tak punya ruang untuk "berhasil, dengan efek samping pincang" pada satu nilai `error`, jadi
setiap lapis di atasnya harus menurunkan ulang bedanya. Handler mencobanya lewat heuristik
`berubah := state≠sebelum || len(history)≠sebelum` (`handler.go:217`), lalu composition root
menambalnya lagi dengan `tolerantDeadlines` + `tolerantTransitionNotifier` yang menelan error
jadi log+metrik. **Tiga lapis menebak-nebak sesuatu yang seharusnya tak perlu ditebak.**

### Cacat kedua: SLA bersifat edge-triggered

Model sekarang: "saat masuk state ber-SLA, buat timer; saat keluar, batalkan." Setiap tepi yang
terlewat adalah kerusakan permanen tanpa jalur pemulihan. Karena itu setiap kegagalan
penjadwalan harus diperlakukan sebagai insiden, dan karena itu pembungkus toleran harus
serba hati-hati. Sistem *level-triggered* — yang membandingkan keadaan yang diinginkan dengan
keadaan teramati lalu memperbaiki selisihnya berulang — boleh ceroboh terhadap kegagalan
tunggal, karena sapuan berikutnya memperbaikinya.

### Daftar cacat mikro yang ditemukan saat analisis

| # | Lokasi | Cacat |
|---|---|---|
| M1 | `engine.go:88` (Start) & `:139` (StartFromTemplate), `handler.go:126` | `Start` menjadwalkan SLA sebelum instance tersimpan |
| M2 | `engine.go:278`, `handler.go:222` | Notifikasi (email, tak reversibel) terkirim sebelum instance tersimpan |
| M3 | `runner.go:214-219` | `advance` menonaktifkan job one-shot **tanpa memandang status**; kegagalan sesaat saat jatuh tempo membuang eskalasi SLA secara permanen. `JobRun.Attempt` ada tapi selalu 1 kecuali `Replay` manual |
| M4 | `engine.go:31-33`, `:277`, `:291`, `:311` | Seam opsional ber-nil: mis-wiring tak bisa dibedakan dari "SLA sengaja mati" |
| M5 | `cmd/server/notification.go`, `scheduler.go` | Kegagalan efek samping hanya log+metrik, dan `/metrics` belum ter-mount (PR-W6a) — hari ini praktis **log-only**. Bersama M3: eskalasi gagal itu tak terlihat DAN tak diulang |
| M6 | `sla_adapter.go:70-80` | `CancelDeadline` read-modify-write tanpa kunci (risiko rendah — key unik per instance+state, tapi polanya rapuh) |
| M7 | `cmd/server/notification.go` | `prepared map[*db.Pool]` tak pernah dievakuasi; bocor bila pool berputar |
| M8 | `infra/db/ensure.go` | DDL *ensure-on-write* dijalankan dari jalur request oleh N goroutine — sumber flake `pg_namespace_nspname_index` (23505) yang kambuh 2× pada suite penuh |

### Ketegangan yang belum pernah diputuskan: pekerjaan latar per-tenant

Setiap mekanisme "pekerjaan latar yang andal per tenant" — relay outbox, penyapu rekonsiliasi,
pembersih idempotency — menuntut kunjungan berkala ke tiap DB tenant. ADR-023 menolak bentuk itu
untuk scheduler dengan alasan anggaran koneksi (`pool_idle` 5 × 20 tenant menabrak
`max_connections`). Ketegangan ini **belum terasa hanya karena tak satu pun mekanisme itu
ter-wire**: `OutboxStore` ada sejak Phase 3 dan sampai hari ini tak punya penulis produksi.

Argumen ADR-023 perlu dipertajam, bukan dibatalkan: yang menabrak tembok bukan *iterasi
per-tenant*, melainkan **pool per-tenant yang ditahan hidup permanen**. Antrean kerja dengan
kolam worker membuat anggaran koneksi menjadi **O(worker), bukan O(tenant)** — tenant ke-500
tak menambah satu koneksi pun selama worker-nya tetap 8.

### Prior art yang dirujuk

- **Outbox transaksional** (Debezium, pola outbox Kafka): efek samping ditulis sebagai baris
  dalam transaksi yang sama dengan baris domain; relay terpisah yang mengeksekusinya.
- **Temporal/Cadence**: kode workflow deterministik & bebas efek samping; tiap efek samping
  adalah Activity dengan retry policy milik engine. Yang diambil adalah **pemisahan
  "memutuskan" dari "bertindak"**, bukan mesinnya.
- **Pola Decider** (`decide(state, command) -> []Event`, murni): bentuk ringan dari hal yang
  sama. `after_commit` Rails adalah versi kasarnya — tak ada efek samping sebelum commit.
- **Controller Kubernetes**: rekonsiliasi level-triggered; tepi yang hilang sembuh sendiri.
- **Null Object** (`io.Discard`, no-op TracerProvider OpenTelemetry): dependensi opsional selalu
  non-nil, sehingga kode terpakai nol pemeriksaan nil.
- **Odoo `Registry(db_name)`** (per-DB, malas, ber-cache, invalidasi lewat bump sequence) dan
  **scheduler per-site Frappe** (antrean sentral, worker membuka DB site hanya selama memproses):
  dua jawaban berbeda untuk "tumpukan per-tenant", dan Frappe adalah yang berskala.

## Keputusan

### K1 — Engine mengembalikan NIAT efek samping, tidak menjalankannya

`ExecuteRequest` berubah menjadi mengembalikan `(TransitionOutcome, error)`, di mana
`TransitionOutcome` memuat daftar efek yang diniatkan:

```go
type Effect interface{ isEffect() }

type ScheduleDeadlineEffect struct{ Deadline Deadline }
type CancelDeadlineEffect   struct{ Key string }
type NotifyEffect           struct{ Spec NotifySpec; InstanceID uuid.UUID; State string }

type TransitionOutcome struct {
    Changed bool
    Effects []Effect
}
```

Engine kembali murni: ia tak memanggil scheduler, tak memanggil notifier, tak menyentuh DB
mana pun. `error` darinya kembali berarti **satu hal saja**: transisi ditolak, tak ada apa pun
yang terjadi. Heuristik `berubah` di handler dan kedua pembungkus toleran **dihapus**.

Ini juga menutup celah yang tak pernah disebut: engine yang bisa menulis ke DB sentral adalah
engine yang bisa dipakai satu tenant untuk menyentuh baris tenant lain. Engine yang murni
tak punya permukaan itu sama sekali.

### K2 — Efek samping ditulis ke outbox TENANT, dalam transaksi yang sama dengan instance

Handler menyimpan instance dan menulis baris efek **dalam satu transaksi DB tenant**:

```
BEGIN (tenant DB)
  INSERT/UPDATE gov.workflow_instances   -- baris otoritatif
  INSERT gov.workflow_effects            -- niat efek samping, N baris
COMMIT
```

Setelah commit, transisi dan niat efeknya durable bersama-sama. Tak ada lagi kemungkinan
"email terkirim untuk transisi yang hilang" maupun "deadline untuk instance yang tak ada".

Eksekusi efek dilakukan **sesudah** commit oleh relay (K3), at-least-once. Karena itu setiap
efek wajib idempoten: `ScheduleDeadline` sudah idempoten (ID job deterministik lewat
`jobIDForKey`), `CancelDeadline` sudah dinyatakan idempoten di port-nya, `Notify` di-dedup
lewat kunci `(instance_id, transition_seq, to_role, channel)` — `transition_seq` adalah kolom
BARU (indeks transisi dalam `History`), belum ada hari ini.

**URUTAN PER-INSTANCE WAJIB, dan idempotensi saja tidak cukup.** Satu transisi self-loop
menghasilkan `CancelDeadlineEffect(key)` lalu `ScheduleDeadlineEffect(key)` dengan `jobIDForKey`
yang **sama persis**. Diproses terbalik, hasil akhirnya adalah deadline yang nonaktif padahal
seharusnya hidup — dan tiap efeknya sendiri tetap idempoten. Relay karena itu memproses efek
**berurut `(instance_id, seq)`**, dan satu efek yang macet menahan efek berikutnya untuk instance
ITU saja (bukan untuk tenant, apalagi antrean global). Konsekuensinya: efek beracun = satu
instance yang macet, terlihat sebagai lag per-instance, bukan antrean yang berhenti.

**Duplikasi email diterima secara sadar.** Dedup `Notify` adalah baris yang ditulis SETELAH
channel mengirim; crash di antara keduanya mengirim ulang saat retry. Itu dual-write yang tak
bisa dihapus tanpa transaksi terdistribusi ke server SMTP. Yang ditukar: dari "email terkirim
untuk transisi yang TIDAK terjadi" (salah, tak bisa dicabut) menjadi "email yang benar bisa
terkirim dua kali" (mengganggu, tidak salah). Untuk notifikasi persuratan, pertukaran itu jelas
menguntungkan — tapi ia pertukaran, bukan kemenangan bersih.

**Konsekuensi yang paling penting: pertanyaan "200 atau 5xx" lenyap.** Tak ada lagi efek
samping pasca-commit di jalur request, jadi tak ada lagi hasil yang perlu dibohongi.

Notifikasi menjadi asinkron — jeda satu interval relay. Untuk notifikasi persuratan
pemerintahan itu bukan regresi yang berarti; kepastian bahwa notifikasi **tak pernah**
mendahului fakta jauh lebih berharga daripada latensi sub-detik.

### K3 — Satu penyapu tenant bersama, anggaran koneksi O(worker)

Semua pekerjaan latar per-tenant memakai **satu** komponen, bukan masing-masing membuat
sendiri: relay outbox event, relay efek workflow, rekonsiliasi SLA, pembersihan idempotency,
dan apa pun berikutnya.

```
TenantSweeper
  - membaca daftar tenant dari id.tenant_registry (sumber yang sama dengan resolver runtime)
  - kolam worker berukuran TETAP (default 8, dari config)
  - tiap worker: pinjam pool tenant → kerjakan → LEPAS
  - pool tenant dari cache ber-LRU dengan batas eksplisit, bukan peta tanpa batas
  - tiap unit kerja adalah SweepTask ber-registry (pola registry #1 CLAUDE.md)
```

Anggaran koneksi menjadi `worker × pool_idle`, konstan terhadap jumlah tenant. Ini **menajamkan
alasan ADR-023, bukan membatalkan keputusannya**: tabel scheduler tetap di DB sentral karena
pembacanya (`Runner.RunDue`) memang loop proses-lebar tanpa tenant. Yang direvisi adalah
kalimat "iterasi per-tenant menabrak anggaran koneksi" — yang menabrak adalah pool permanen
per-tenant, dan itu bisa dihindari.

### K4 — SLA menjadi level-triggered; penjadwalan adalah optimasi, rekonsiliasi adalah kebenaran

`SLAReconcileTask` (satu `SweepTask`) berjalan berkala per tenant:

1. Baca instance terbuka yang state-nya ber-`sla_hours > 0` (butuh method baru pada
   `InstanceStore`, lihat gap G4).
2. Hitung deadline yang **seharusnya** = waktu masuk state (dari `History`) + `sla_hours`.
3. Bandingkan dengan `gov.scheduled_jobs` sentral; buat yang hilang, nonaktifkan yang yatim.

Dengan ini:
- Deadline yang gagal dijadwalkan **sembuh sendiri** — butir backlog "tak ada recovery" tertutup
  bukan dengan menambah penanganan error, tapi dengan menghapus kelas masalahnya.
- `CancelDeadline` yang gagal berhenti menjadi soal kebenaran (guard race sudah jadi backstop;
  rekonsiliasi jadi backstop kedua).
- Kerapuhan urutan cancel→schedule pada self-loop kehilangan taringnya.

### K5 — Seam opsional memakai Null Object, bukan nil

`WithDeadlines`/`WithNotifier` tetap ada, tapi field-nya selalu non-nil (default no-op).
Empat pemeriksaan nil hilang dari engine, dan "terpasang atau dorman" menjadi murni urusan
composition root — persis semangat Aturan 11 DoD.

### K6 — Job one-shot yang gagal punya kebijakan retry eksplisit

`advance` berhenti menonaktifkan one-shot tanpa memandang status. Kebijakan: backoff
eksponensial dengan batas percobaan (default 5), lalu tandai `exhausted` — **bukan** dibuang
diam-diam. Batas itu aman justru karena K4 ada: rekonsiliasi adalah jaring terakhir.

### K7 — DDL keluar dari jalur request (jalur terpisah)

`EnsureSchemaLocked` dari jalur request diganti pola deploy-time: provisioning membuat skema,
aplikasi **memeriksa dan gagal cepat** bila skema tak sesuai (pola `migrate --check` Django /
penolakan migrasi tertunda Rails). Ini menutup kelas flake M8, bukan menambalnya dengan kunci
yang lebih rapat. Dipisah sebagai track sendiri karena menyentuh provisioning tenant, bukan
workflow.

## Perbandingan dengan kode yang sudah dibangun

| Yang dibutuhkan | Status hari ini | Tindakan |
|---|---|---|
| Outbox transaksional | `infra/eventbus/outbox.go` **ada lengkap** (store + relay + retry policy + DLQ), tanpa penulis produksi | Pakai ulang bentuknya untuk `gov.workflow_effects`; jangan bikin mekanisme kedua |
| ID job deterministik utk idempotensi | `jobIDForKey` (UUIDv5) **sudah ada** | Dipakai apa adanya |
| Guard race fire-time | `EscalationCoordinator` **sudah ada & teruji** | Tetap; jadi lapis pertama, rekonsiliasi lapis kedua |
| Residensi jadwal di DB sentral | ADR-023, **sudah jalan** | Tetap. Hanya alasan anggaran koneksinya yang di-amend |
| Runner + lock ber-sewa + `WithoutCancel` | PR-W4b, **sudah jalan & teruji** | Tetap seluruhnya |
| Binding peran beku di instance | ADR-012, **sudah jalan** | Tetap; `NotifyEffect` membawa peran konkret hasil binding |
| Registry `SweepTask` per-tenant | **tidak ada** | Bangun (G1) |
| Kolam pool tenant berbatas | `TenantConnManager` meng-cache per (host,dbname) **tanpa batas & tanpa evakuasi** | Tambah batas + LRU (G2) |
| Query "instance terbuka di state ber-SLA" | **tidak ada** di `InstanceStore` | Tambah method (G4) |
| Tabel `gov.workflow_effects` | **tidak ada** | DDL baru + DB_SCHEMA/DB_CHANGELOG |
| `tolerantDeadlines` / `tolerantTransitionNotifier` | PR-W4b | **Dihapus** setelah K2 |
| Heuristik `berubah` di handler | PR-W4a | **Dihapus** setelah K1 |
| Metrik `workflow_*_failed_total` | PR-W4b, belum ter-expose | Tetap berguna; sasarannya berpindah ke relay |
| e2e `sla_notification_e2e_integration_test.go` | PR-W4b, hijau | Tetap; disesuaikan agar menunggu relay, bukan efek sinkron |

**Yang tidak dibuang dari PR-W4b:** residensi sentral, runner, lock, seeder framework,
pagar tenant lintas-DB di `EscalationJob`, dan e2e-nya. Yang dibuang justru tepat tambalan
yang membuatnya terasa ruwet.

## Konsekuensi

**Yang membaik.** Tak ada lagi dual-write di jalur transisi. `error` engine kembali punya satu
makna. Tiga lapis penebak (heuristik handler + dua pembungkus toleran) hilang, bukan bertambah.
Tepi yang terlewat sembuh sendiri. Anggaran koneksi berhenti tumbuh mengikuti jumlah tenant.
Engine yang murni kembali bisa diuji tanpa DB apa pun.

**Yang memburuk atau harus diterima.**
- Notifikasi menjadi asinkron; UI yang mengharapkan inbox terisi tepat saat respons 200 harus
  menyesuaikan (belum ada UI — biaya diambil sekarang, bukan nanti). **Peredamnya:** handler
  boleh mencoba memproses efek instance itu **inline tepat setelah commit** (pool tenant sudah
  terbuka di tangannya), dan MENGABAIKAN kegagalannya — baris efeknya tetap di tabel untuk
  disapu relay. Ini pola "outbox + immediate dispatch attempt": latensi jalur normal nyaris
  seperti sekarang, kebenarannya tetap milik tabel. Yang penting: percobaan inline itu tak boleh
  pernah memengaruhi respons — kalau ia bisa, kita membangun ulang cacat yang sedang dihapus.
- Kompleksitas berpindah, tidak menguap: satu subsistem asinkron baru (tabel + relay + urutan +
  DLQ + lag) menggantikan tiga tambalan sinkron. Pertukaran ini sengaja — kegagalan asinkron bisa
  dipulihkan by construction (retry, DLQ, rekonsiliasi), kegagalan sinkron pasca-commit tidak.
- Satu tabel baru per tenant (`gov.workflow_effects`) dan satu proses latar baru yang harus
  dipantau. Relay yang mati = efek yang menumpuk diam-diam; ia **wajib** punya metrik lag dan
  masuk daftar health check sejak PR pertamanya.
- `TransitionOutcome` adalah **perubahan breaking pada interface publik `core/workflow`** —
  memang itu sebabnya ini ADR.
- Rekonsiliasi menambah beban baca berkala pada tiap tenant DB. Dibatasi dengan interval
  konservatif (default 5 menit) dan query ber-index pada state terbuka.

**Yang sengaja tidak diubah.** `TryLockInstance` tetap (ia melindungi hal lain: dua orang
mendisposisi surat yang sama). ADR-022 dispatch bertipe tetap. Residensi ADR-023 tetap.

## Alternatif yang dipertimbangkan

**Mengadopsi Temporal/Cadence.** Ditolak. Model persistennya menggantikan `core/workflow`
seluruhnya, membawa server + worker sendiri, dan konteks pemda satu-binary-satu-Postgres tak
menanggungnya. Pola Decider-nya diambil; mesinnya tidak.

**Pertahankan efek sinkron, cukup benahi urutannya (Save dulu, baru efek).** Ditolak. Ia
memperbaiki M2 dan M1 tapi meninggalkan seluruh sisanya: efek tetap bisa gagal setelah commit,
jadi pembungkus toleran tetap dibutuhkan, "200 atau 5xx" tetap harus dijawab, dan SLA tetap
edge-triggered. Menukar satu cacat dengan mempertahankan lima.

**Rekonsiliasi saja, tanpa outbox.** Ditolak untuk notifikasi. Rekonsiliasi bekerja untuk
keadaan yang bisa diturunkan ulang dari data (deadline **adalah** fungsi dari state + waktu
masuk); ia tak bekerja untuk kejadian yang harus terjadi tepat sekali dan tak punya jejak
keadaan (notifikasi transisi). Outbox untuk kejadian, rekonsiliasi untuk keadaan — keduanya
diperlukan, masing-masing di tempat yang tepat.

**Outbox saja, tanpa rekonsiliasi.** Ditolak. Outbox menjamin efek yang **tercatat** akhirnya
jalan; ia tak menolong bila tepinya sendiri tak pernah tercatat (bug, restart di jendela sempit,
migrasi data). Level-triggered adalah jaring yang tak bergantung pada kebenaran penulisnya.

**Satu relay per tenant sebagai goroutine permanen.** Ditolak dengan alasan yang sama seperti
ADR-023 menolak fasad per-tenant: N goroutine + N pool permanen. Kolam worker berbatas (K3)
memberi hasil yang sama dengan anggaran konstan.

**Menaruh `gov.workflow_effects` di DB sentral agar satu relay saja cukup.** Ditolak. Itu
mengembalikan dual-write yang justru mau dihapus — baris efek harus commit bersama baris
instance, dan baris instance hidup di DB tenant. Residensi mengikuti **transaksi** di sini,
sebagaimana ADR-023 menetapkan residensi mengikuti **pembaca** di sana.
