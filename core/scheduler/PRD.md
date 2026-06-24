# PRD: Scheduler

## Tujuan
Menjalankan pekerjaan terjadwal (laporan berkala, rekonsiliasi) dan deadline-aware
(eskalasi SLA workflow), aman di lingkungan multi-instance.

## Konteks & batasan
### Jadi tanggung jawab
- Cron scheduling, deadline scheduling, distributed lock, job history & replay
### BUKAN tanggung jawab
- Logika job itu sendiri (didefinisikan pemanggil/ modul)
- Notifikasi (core/notification; scheduler memicu)

## Kebutuhan fungsional
- F1: Cron-based scheduling dengan ekspresi standar; riwayat eksekusi.
- F2: Deadline scheduling: daftarkan deadline (dari workflow SLA), callback saat lewat.
- F3: Distributed lock: job yang sama tidak jalan ganda di banyak instance.
- F4: Job history: status (success/failed), timing, error; replay job gagal dengan
  konteks yang sama.

## Kebutuhan non-fungsional
- Akurasi penjadwalan dalam orde detik.
- Lock contention minimal; lock punya TTL agar tidak deadlock bila instance mati.

## Dependency
- infra/cache atau infra/db untuk distributed lock
- core/workflow (deadline SLA), core/notification (eskalasi)

## Anti-pattern
- Job non-idempoten yang di-replay membabi buta.
- Job tanpa lock yang jalan ganda di multi-instance.

## Acceptance criteria
- [ ] Job terjadwal jalan tepat waktu.
- [ ] Dua instance: job jalan sekali (lock bekerja).
- [ ] Deadline SLA lewat → callback eskalasi terpanggil.
- [ ] Job gagal bisa di-replay dengan konteks sama.
