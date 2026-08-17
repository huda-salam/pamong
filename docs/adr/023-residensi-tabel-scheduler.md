# ADR-023: Residensi tabel scheduler — DB sentral, bukan tenant DB

## Status
Accepted

## Konteks

`core/scheduler` lengkap dan teruji sejak PR-3.5.1/3.5.2: `Registry` (JobKey → JobFunc),
`Runner` (poll → eksekusi → catat riwayat → hitung ulang `next_run_at`), `Locker` ber-sewa
untuk multi-instance, adapter Postgres `infra/scheduler.DBJobStore` & `DBLocker`. Tak satu pun
terpasang di server hidup. PR-W4b hendak merakitnya — dan tersandung pada satu hal yang tak
terlihat selama komponen diuji sendiri-sendiri.

Migrasi `core/scheduler/migrations/001` & `002` menempatkan `gov.scheduled_jobs`,
`gov.job_runs`, dan `gov.job_locks` di **tenant DB**, mengikuti konvensi CLAUDE.md bahwa
schema `gov` adalah tabel framework di dalam DB tiap tenant (bersama `gov.audit_logs`,
`gov.workflow_definitions`, `gov.tenant_configs`).

Tapi `Runner.RunDue` memanggil `store.DueSchedules(ctx, now)` **satu kali per tick**, dari
goroutine ticker proses-lebar. Pemanggil itu tidak berada di dalam tenant mana pun; ia bertanya
"apa yang jatuh tempo, **di mana saja**?". Satu `JobStore` di atas satu pool tenant tidak bisa
menjawabnya.

Dua kandidat ditimbang:

**(a) Fasad JobStore yang meng-iterasi tenant.** Tiap tick, baca daftar tenant dari
`id.tenant_registry`, resolve pool tiap tenant, `DueSchedules` per tenant, gabungkan. Residensi
tetap; tanpa migrasi; tanpa ADR.

**(b) Pindahkan ketiga tabel ke DB sentral.** Satu pool, satu locker, satu query per tick.
`ScheduledJob.TenantID` dipakai me-route eksekusi handler lewat `port.WithTenant`.

## Keputusan

**Opsi (b): tabel scheduler hidup di DB sentral (`CentralDBResolved`, ADR-005), bukan di
tenant DB.** Tiga alasan, diurut dari yang paling menentukan.

**1. Residensi mengikuti PEMBACA, bukan penulis.**

Ini prinsip yang ADR ini tetapkan dan yang bisa dipakai ulang. Tenant DB adalah tempat yang
tepat ketika pembaca tabel itu **selalu** berada di dalam request satu tenant — berlaku untuk
`gov.audit_logs`, `gov.workflow_definitions`, `gov.tenant_configs`, `gov.workflow_instances`.
Due-scan scheduler tidak punya tenant: ia adalah loop platform. Memaksa pembaca tanpa-tenant
mensintesis loop tenant adalah dari mana seluruh biaya opsi (a) berasal.

Penulisnya memang ber-tenant (`SchedulerDeadlines.ScheduleDeadline` dipanggil di tengah
transisi, dari request), tapi penulis yang ber-tenant sudah membawa identitas tenant di
barisnya. Pembaca yang tanpa-tenant tidak punya apa pun untuk direkonstruksi.

**2. Tipe datanya memang sudah ditulis untuk tabel sentral.**

`ScheduledJob.TenantID` dan `JobRun.TenantID` ada sejak PR-3.5.1, dan kolomnya berkomentar
`kosong = job level-platform`. Di tenant DB kolom itu **redundan** (selalu sama dengan DB
tempat ia dibaca) sekaligus **inkoheren**: job level-platform tak punya rumah — ia tergandakan
di tiap tenant DB atau tak ada sama sekali.

Ini kontras tegas dengan ADR-022, yang memilih tumpukan per-tenant untuk workflow justru
karena `DefinitionStore`/`TemplateStore` **tidak membawa `ctx` maupun tenant sama sekali** —
tak ada nilai apa pun untuk memilih DB. Scheduler kebalikannya. Presedennya tidak berlaku, dan
menyalinnya buta akan salah.

**3. Anggaran koneksi menutup opsi (a) jauh lebih cepat dari dugaan.**

Pool di-cache per `(host, dbname)` (`infra/db/conn_manager.go`) dengan `MinConns` =
`pool_idle` = 5 (`config/default.yaml`). Di Tier 1 semua tenant satu host tapi **beda dbname**,
jadi satu pool per tenant. Yang menentukan: hari ini pool dibuka **malas** — tenant tanpa
trafik tak punya pool. Fan-out scheduler mengubahnya menjadi "setiap tenant, setiap tick,
selamanya": 20 tenant = 100 koneksi idle (tepat di `max_connections` default), 550 tenant
(38 provinsi + 514 kab/kota) = 2.750.

Biayanya bukan N query per tick — 550 tenant pada tick 30 detik hanya 18 qps, remeh bagi
Postgres. Biayanya konversi pool malas menjadi pool permanen, yang mengubah leher botol
`MinConns × tenant` dari laten menjadi pasti. Mitigasinya ada (PgBouncer, koneksi non-pool
khusus due-scan, `pool_idle: 0`), tapi masing-masing adalah mesin tambahan: **opsi (a) hanya
murah selama anggaran koneksi tidak dihitung.**

**4. Payload job adalah RUJUKAN, bukan isi — dan itu wajib dijaga.**

Konsekuensi (b) adalah data ber-tenant menumpang DB bersama, jadi batasnya ditetapkan di sini:
payload job hanya boleh memuat **pengenal** (UUID instance, nama state, nama role, tenant_id),
tidak pernah isi dokumen, nama orang, atau field ber-`DataClass` `personal_id`/`specific`
(ADR-009). `coreWf.Escalation` hari ini memenuhi batas itu apa adanya. Job baru yang butuh
konteks lebih kaya **membacanya dari tenant DB saat handler berjalan**, dengan tenant yang
sudah ada di `ctx` — bukan dengan menggemukkan payload.

**5. Handler menerima tenant lewat `ctx`, bukan lewat parameter baru.**

`Runner` menyisipkan `port.WithTenant(ctx, job.TenantID)` sebelum memanggil `JobFunc`.
Signature `JobFunc(ctx, payload)` tidak berubah, dan handler memperoleh routing DB per-tenant
lewat jalur yang sama persis dengan handler HTTP. Ini dibutuhkan pada **kedua** opsi — di (a)
pun handler harus tahu tenant mana yang sedang dilayani — jadi ia bukan biaya opsi (b).

**6. Migrasi punya dua jalur, dan `pamongctl migrate` harus membedakannya.**

`infra/schema.CoreMigrations()` mengumpulkan migrasi semua komponen core dan `pamongctl
migrate up` menerapkannya ke DB tenant. Tanpa perubahan, tabel scheduler akan tetap dibuat di
tiap tenant DB — tak berbahaya, tapi persis kebohongan yang membuat cacat residensi sulit
dilihat. Karena itu komponen core dipecah menjadi **tenant-resident** dan **central-resident**,
dan `pamongctl migrate ... --central` menerapkan yang kedua ke DB sentral.

**7. Nama schema tetap `gov`.**

`gov` menandai "tabel framework", bukan "tabel tenant"; residensi adalah properti DB mana,
bukan nama schema (konsisten ADR-005, di mana residensi adalah properti entity). Menamai ulang
akan menuntut migrasi ke-003 yang membatalkan penempatan 001/002 — file migrasi bersifat
append-only, dan tiga migrasi yang saling meniadakan lebih membingungkan daripada satu nama
schema yang dipakai di dua DB. Pemisahnya adalah **jalur migrasi**, bukan nama.

## Konsekuensi

**Diterima:**

- Data operasional ber-tenant (jadwal + riwayat eksekusi) hidup di DB bersama. Diterima karena
  isinya rujukan, dan dipagari Keputusan 4.
- `gov.job_runs` sentral tumbuh sebagai gabungan seluruh tenant. Retensi/partisi belum
  diperlukan sekarang; dicatat sebagai utang bila volume naik.
- Konvensi "tabel `gov.*` ada di tenant DB" kini punya pengecualian ber-nama. Prinsip
  Keputusan 1 adalah yang menentukan mana yang mana, bukan daftar hafalan.
- Tenant yang naik ke Tier 2/3 meninggalkan jadwalnya di DB sentral. Ini justru menghapus
  jendela cutover `pg_dump` (job yang tertulis setelah dump tidak hilang), dan runner tetap
  menemukan DB baru lewat registry saat handler berjalan.

**Diperoleh:**

- Due-scan O(1) query per tick, satu pool, satu `job_locks` — tak ada pool tenant yang
  dipaksa hidup oleh scheduler.
- Job level-platform (`tenant_id = ''`) punya rumah yang koheren.
- `SchedulerDeadlines` tetap SATU instance bersama; ia tak perlu dibangun per-tenant di dalam
  `workflowFactory.RuntimeFor` seperti yang dituntut opsi (a).

**Risiko yang disadari:**

- DB sentral menerima tulis dari setiap transisi ber-SLA di setiap tenant. Beban ini kecil
  (satu upsert per deadline) tapi menambah kopling ke DB yang sudah jadi perhatian beban.
  Bila kelak jadi masalah, jalan keluarnya adalah memisah `gov_central` dari identity DB —
  jalur yang sudah disiapkan `CentralDBResolved` dan tak menuntut perubahan kode.
- Runner mati = SLA seluruh tenant berhenti (di (a) juga demikian, karena registry tenant pun
  hidup di DB sentral — tak ada keunggulan isolasi kegagalan yang hilang di sini).

## Alternatif yang dipertimbangkan

**(a) Fasad JobStore per-tenant.** Ditolak karena Keputusan 1–3. Preseden strukturalnya
adalah Odoo, yang menaruh `ir.cron` di tiap DB tenant dan meng-iterasi `db_list` tiap siklus —
dan yang temboknya pada jumlah DB tinggi terdokumentasi. Frappe/ERPNext memilih pola (b)
(antrean sentral, `site` dibawa di job, data situs tetap per-DB) dan itulah yang menjalankan
ribuan situs.

**(c) Indeks due sentral + payload tetap di tenant DB.** Runner memindai satu indeks ringan
`(tenant_id, job_id, next_run_at)` lalu membaca job penuh dari tenant DB. Memberi O(1) scan
tanpa memindahkan payload, tapi menuntut dua tulis yang harus konsisten pada tiap penjadwalan.
Ditolak: kompleksitasnya membeli perlindungan atas payload yang — per Keputusan 4 — memang
tidak boleh memuat apa pun yang perlu dilindungi.

**(d) Tunda dengan menjalankan scheduler single-tenant dulu.** Ditolak: itu memindahkan
keputusan yang sama ke titik di mana sudah ada data produksi untuk dimigrasikan. Tabelnya
kosong sekarang; ini saat termurah untuk memilih.
