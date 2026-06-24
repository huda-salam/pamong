# PRD: Notification Hub

## Tujuan
Mengirim notifikasi lintas channel (email, push, in-app) berdasarkan peran/jabatan,
dengan template yang bisa dikustomisasi per-tenant. Routing berbasis peran memastikan
notifikasi sampai ke pejabat yang tepat meski orangnya berganti (mutasi/PLT).

## Konteks & batasan
### Jadi tanggung jawab
- Abstraksi & registry channel; template; routing peran; delivery tracking
### BUKAN tanggung jawab
- Pengiriman fisik (itu infra/messaging adapter)
- Resolusi peran->orang (delegasi ke core/permission; hub memanggil)

## Kebutuhan fungsional
- F1: Channel abstraction; channel (email/push/in-app) didaftarkan ke registry; tambah
  channel baru tanpa ubah pemanggil.
- F2: Template engine per-tenant + i18n (Indonesia & lokal).
- F3: Routing ke role/jabatan, bukan user spesifik. Resolusi ke orang saat kirim;
  bila jabatan kosong → fallback ke PLT.
- F4: Delivery tracking: status read/delivered/failed, dapat di-audit.

## Kebutuhan non-fungsional
- Pengiriman asinkron (via event bus / queue), tidak memblok use case.
- Retry untuk channel yang gagal sementara.

## Dependency
- core/permission — resolusi peran→orang (+ PLT)
- infra/messaging — adapter channel
- Event bus — pemicu notifikasi & pengiriman async

## Anti-pattern
- Notify ke person_id hardcoded (bukan peran) → rusak saat mutasi.
- Channel di-hardcode di pemanggil (bukan via registry).

## Acceptance criteria
- [ ] Kirim notif in-app & email (mock) dengan template benar per-tenant.
- [ ] Notif ke "Kadis" jatuh ke PLT bila jabatan kosong.
- [ ] Channel baru ditambah lewat registry tanpa ubah pemanggil.
- [ ] Status pengiriman terlacak.
