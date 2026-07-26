# PRD: Tenant Customization Layer

## Tujuan
Memungkinkan kustomisasi per-tenant (tambah field, ubah label, aktifkan fitur) tanpa
mengubah kode/definisi modul inti, dan tanpa upgrade framework menimpa kustomisasi.
Permintaan "tambah field ini" / "ubah label itu" dari pemda pasti datang dan tidak boleh
memerlukan perubahan kode modul.

## Konteks & batasan
### Jadi tanggung jawab
- Custom field per-tenant; label/tampilan override; capability flags; merge runtime
### BUKAN tanggung jawab
- Definisi entity inti (core/domain)
- Logika bisnis (kustomisasi bersifat deklaratif: field/label/flag, bukan kode)

## Kebutuhan fungsional
- F1: Custom field per-tenant disimpan di gov.tenant_customizations, terpisah dari
  definisi modul; di-merge dengan EntityDef saat runtime (untuk form, validasi, storage).
- F2: Label/tampilan override per-tenant (mis. ubah label field, urutan tampil).
- F3: Capability flags per-tenant: aktifkan/nonaktifkan fitur dormant tanpa rilis
  terpisah atau percabangan kode yang menyebar.
- F4: Upgrade-safe: upgrade framework/modul tidak menimpa kustomisasi tenant; kustomisasi
  tidak mengotori definisi inti.

## Kebutuhan non-fungsional
- Merge definisi inti + kustomisasi: hasil di-cache per-tenant, invalidasi saat berubah.
- Custom field tidak boleh menurunkan integritas (tetap tervalidasi seperti field inti).

## Dependency
- core/domain — EntityDef yang di-extend
- Event bus — invalidasi cache merge saat kustomisasi berubah

## Anti-pattern
- Menyimpan kustomisasi di tabel modul inti (rusak saat upgrade).
- Custom field bentrok nama dengan field inti (perlu namespace/validasi).
- Capability via percabangan kode tersebar (gunakan flag terpusat).

## Acceptance criteria
- [x] Tenant menambah custom field tanpa mengubah definisi modul; field tersimpan &
      di-merge ke EntityDef saat runtime (MergeEntity/EffectiveEntity). (PR-3.4.1)
      Penerapan ke form dirender lapis UI saat FieldUI hadir — DEFERRED(Phase-3.8/UI).
- [x] Upgrade modul tidak menimpa custom field/label tenant (layer terpisah:
      gov.tenant_custom_fields + tenant config). (PR-3.4.1)
- [x] Label override per-tenant tersimpan & ter-resolve (LabelResolver di atas config
      ber-scope). Penerapan ke field render menyusul FieldUI — DEFERRED. (PR-3.4.1)
- [x] Capability flag mengaktifkan/menonaktifkan fitur per-tenant tanpa rilis terpisah. (PR-3.4.2)
      Persistensi (gov.tenant_capability_overrides) + jalur tulis ber-permission. (PR-3.4.1)
