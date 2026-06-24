# core/scheduler — Scheduler

Penjadwalan job: cron, deadline-aware (untuk SLA workflow), distributed lock (anti
double-run multi-instance), job history & replay.

## Bergantung pada
- port/ + stdlib; infra (lock via cache/db), core/workflow (SLA callback)

## Tanggung jawab
- Cron-based scheduling (laporan berkala, rekonsiliasi)
- Deadline scheduling untuk SLA workflow (eskalasi)
- Distributed lock: job tidak jalan ganda di multi-instance
- Job history & replay job yang gagal

## File kunci
- cron.go — penjadwalan berbasis cron
- queue.go — job queue & eksekusi
- lock.go — distributed lock
- history.go — riwayat & replay

## Konvensi khusus
- Job idempoten bila memungkinkan (replay aman).
- Distributed lock wajib untuk job yang tak boleh ganda.

## Test
- Unit: penjadwalan, lock (dua instance -> job sekali), replay.
- go test ./core/scheduler/... -race

## Rujukan
- PRD.md, core/workflow/PRD.md (SLA)
