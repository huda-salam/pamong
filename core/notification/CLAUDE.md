# core/notification — Notification Hub

Channel abstraction (email, push, in-app — pluggable via registry). Template per-tenant,
i18n. Routing ke PERAN/jabatan (bukan user spesifik) dengan fallback ke PLT. Delivery
tracking.

## Bergantung pada
- port/ + stdlib; core/permission (resolusi peran->orang), infra/messaging (channel)

## Tanggung jawab
- Channel abstraction + registry (tambah channel tanpa ubah pemanggil)
- Template engine per-tenant, i18n
- Routing by role/jabatan, fallback PLT
- Delivery tracking (read/delivered/failed)

## File kunci
- hub.go — entry kirim notifikasi
- channel.go — interface channel + registry
- template.go — template engine per-tenant, i18n
- routing.go — resolusi peran -> penerima (fallback PLT)
- tracking.go — status pengiriman

## Konvensi khusus
- Notify ke peran, bukan person_id. Resolusi ke orang saat kirim (termasuk PLT).
- Channel baru = daftar ke registry (open for extension).

## Test
- Unit: template render, routing peran->orang (fallback PLT), channel registry.
- go test ./core/notification/... -race

## Rujukan
- PRD.md, core/permission/PRD.md (resolusi peran)
