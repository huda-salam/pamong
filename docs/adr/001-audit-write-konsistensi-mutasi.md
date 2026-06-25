# ADR-001: Konsistensi penulisan audit terhadap mutasi data

## Status
Accepted

## Konteks
Setiap mutasi entity `Auditable` harus menghasilkan jejak audit (`gov.audit_logs`).
Untuk konteks pemerintahan (pemeriksaan BPK/BPKP), prinsipnya keras: **tidak boleh ada
mutasi tanpa jejak**.

Pada Phase 1.3, audit dibangun sebagai dekorator repository (`auditedRepo` di
`infra/db`). Alur saat ini:

```
Save(entity)  →  SQLRepository.Save(...)   // transaksi #1: data bisnis, commit
              →  Engine.Record(...)         // transaksi #2: gov.audit_logs, commit
```

Dua transaksi terpisah. `port.BaseRepository` tidak mengekspos batas transaksi, jadi
mutasi dan audit tidak bisa commit bersama tanpa mengubah kontrak repository. Akibatnya
ada celah teoretis: mutasi commit, lalu proses crash / DB error sebelum audit ditulis →
mutasi tanpa jejak. (Advisory lock per tenant yang sudah ada hanya menjaga hash chain
tidak putus oleh penulisan paralel — bukan menjembatani konsistensi mutasi↔audit.)

## Keputusan
Untuk Phase 1, **terima penulisan audit di transaksi terpisah** dan tutup celah
konsistensi lewat **outbox pattern di PR-3.1.2**: di dalam transaksi mutasi, audit
ditulis sebagai intent ke tabel outbox (atomik dengan mutasi), lalu di-relay andal oleh
proses terpisah. Audit Phase 1 sudah fungsional dan teruji; jaminan atomik bukan
prasyarat untuk melanjutkan ke Phase 2.

## Konsekuensi
- Selama window sebelum 3.1.2, ada kemungkinan (kecil) mutasi tercatat tanpa audit bila
  proses gagal tepat di antara dua transaksi. Risiko ini diterima secara sadar dan
  didokumentasikan di sini, bukan diabaikan diam-diam.
- Integritas isi audit (hash chain, tamper detection) **tidak** terdampak — yang belum
  dijamin hanyalah kelengkapan (no-gap).
- PR-3.1.2 wajib menutup celah ini; ADR ini menjadi rujukan kenapa outbox dipilih.

## Alternatif yang dipertimbangkan
- **Unit of Work / same-transaction** (repo berbagi satu `Tx`, mutasi + audit commit
  bersama). Memberi atomik penuh sekarang, tapi mengubah kontrak `port.BaseRepository`
  secara fundamental (semua mutasi harus sadar-transaksi). Ditolak untuk sekarang:
  perubahan terlalu invasif untuk Phase 1, dan outbox sudah direncanakan memberi jaminan
  setara dengan kopling lebih longgar (siap untuk event bus & ekstraksi microservice).
- **Membiarkan tanpa rencana penutupan.** Ditolak — melanggar prinsip no-gap audit.
