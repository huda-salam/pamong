# infra/crypto — Crypto Adapter

Driven adapter: implementasi `port.CryptoPort` (ADR-009). Enkripsi field selektif
(AES-256-GCM) + blind index (HMAC-SHA256) untuk equality/UNIQUE. Envelope encryption
KEK→DEK dengan KMS (ADR-010). SENSITIF — perubahan menyentuh keamanan data pribadi.

## Status
DEFERRED — diimplementasi pasca-Phase 3 (lihat ROADMAP sub-phase kripto). Dokumen ini
menetapkan kontrak & seam agar `port/crypto.go` dan `FieldDef.Class` bisa masuk lebih dulu.

## Bergantung pada
- port/crypto.go; pustaka crypto stdlib + klien KMS (Vault Transit / lainnya — ADR-010)

## Tidak boleh
- Kunci mentah (KEK/DEK/bidx key) tersimpan di DB tenant atau di-log
- Dekripsi tanpa tenantID (hierarki DEK per-tenant)
- Dipanggil dari use case/domain — enkripsi otomatis di lapis repository (infra/db)

## Tanggung jawab
- Encrypt/Decrypt AES-256-GCM, nonce acak per-nilai
- BlindIndex HMAC-SHA256 (kunci terpisah dari kunci enkripsi) untuk equality & UNIQUE
- Format ciphertext self-describing: v1|key_id|key_version|nonce|ct+tag (rotasi tanpa migrasi)
- Envelope: KEK di `KeyProvider` (KMS) membungkus DEK per-tenant per-purpose (ADR-010)
- KMS = driver ber-registry (`GOV_CRYPTO_KMS_DRIVER`), pola eventbus/storage. Tambah KMS =
  impl `KeyProvider` + `Register()`, tanpa ubah kode kripto.
- Custody = kebijakan per-tenant (`key_custody`: platform|tenant) → pilih KeyProvider.
- Driver:
  - `static` — KMS-alike bawaan, **default produksi Tier 1/2**. Master KEK dari secret
    (`GOV_CRYPTO_MASTER_KEY`, ber-versi), envelope in-app, DEK ter-wrap di `id.data_keys`.
    Tanpa dependensi eksternal. Postur: lindungi master key (ops); ganti HSM/Vault = ganti driver.
  - `local` — dev/test SAJA (kunci config, tanpa versi), ditolak bila production.
  - `vault`/`aws-kms`/`bssn`/... — di-plug saat pengadaan menentukan.

## File kunci (rencana)
- crypto.go — entry, impl CryptoPort (AES-GCM + blind index)
- provider.go — interface KeyProvider + registry driver KMS
- drivers/static.go — KMS-alike bawaan (master KEK ber-versi dari secret; default produksi)
- drivers/local.go — dev/test (kunci statis dari config, tanpa versi)
- drivers/vault.go — Vault Transit (di-plug saat pengadaan menentukan)
- dek_store.go — baca/tulis DEK ter-wrap di id.data_keys (identity DB, sentral)
- envelope.go — hierarki KEK→DEK, wrap/unwrap, cache DEK ter-decrypt (in-proc, TTL)
- custody.go — resolusi key_custody per-tenant → pilih KeyProvider

## Konvensi khusus
- `purpose` memisahkan konteks kunci (mis. "nik" vs "no_rekening") tanpa ubah port.
- Kunci blind-index TERPISAH dari kunci enkripsi. Rotasi bidx = reindex seluruh baris (mahal).
- Ciphertext membawa key_version → dekripsi lama tetap jalan saat rotasi (lazy re-encrypt).

## Pitfall umum
- Deterministic encryption untuk equality (membocorkan kesamaan nilai) — pakai GCM + blind
  index, bukan itu.
- Menyimpan DEK ter-wrap di tenant DB (dump membuka jalan) — DEK di sentral/KMS.
- Lupa menutup jalur kebocoran samping (audit diff, event, idempotency, log) — enkripsi
  kolom saja = teater keamanan (ADR-009 §6).

## Test
- Unit: roundtrip enc/dec; blind index deterministik untuk nilai sama; format ciphertext.
- Integration (Vault test container): wrap/unwrap DEK, rotasi key_version.

## Rujukan
- PRD.md, port/crypto.go, docs/adr/009-*, docs/adr/010-*, ADR-002 (audit diff)
