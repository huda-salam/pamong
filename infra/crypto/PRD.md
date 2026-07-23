# PRD: Crypto Adapter

## Tujuan
Mengimplementasi `port.CryptoPort` (ADR-009): enkripsi field selektif at-rest (AES-256-GCM)
dengan blind index (HMAC-SHA256) untuk mempertahankan equality lookup & UNIQUE, di atas
envelope encryption KEK→DEK per-tenant per-purpose (ADR-010). Domain & use case tetap
nol-dependency kripto — pemanggilan otomatis di lapis repository (infra/db).

## Status
DEFERRED — implementasi pasca-Phase 3, sebelum tenant produksi pertama. PRD ini mengunci
kontrak agar `port/crypto.go`, `FieldDef.Class`, dan DDL multi-kolom bisa masuk lebih dulu
tanpa biaya migrasi belakangan.

## Kebutuhan fungsional
- F1: `Encrypt(ctx, tenantID, purpose, plain) → ct` — AES-256-GCM, nonce acak per-nilai;
  output ber-format self-describing `v1|key_id|key_version|nonce|ct+tag`.
- F2: `Decrypt(ctx, tenantID, ct) → plain` — baca metadata dari ciphertext (termasuk
  key_version) untuk memilih DEK yang tepat; lazy re-encrypt bukan tanggung jawab port.
- F3: `BlindIndex(ctx, tenantID, purpose, plain) → bidx` — HMAC-SHA256 atas nilai
  ter-normalisasi (trim/case per-purpose); deterministik agar equality & UNIQUE bekerja;
  kunci bidx TERPISAH dari kunci enkripsi.
- F4: Envelope (ADR-010): KEK di `KeyProvider` (KMS) membungkus DEK per-(tenant, purpose);
  DEK ter-wrap disimpan **sentral** di `id.data_keys` (identity DB), bukan tenant DB; cache
  DEK ter-decrypt in-process ber-TTL.
- F5: **KMS = driver ber-registry (ADR-010)**, pola eventbus/storage. `KeyProvider`
  (WrapDEK/UnwrapDEK/GenerateDEK) dipilih via `GOV_CRYPTO_KMS_DRIVER`. Driver:
  - `static` — **KMS-alike bawaan, default produksi Tier 1/2.** Master KEK dari secret source
    (`GOV_CRYPTO_MASTER_KEY`, base64 32-byte, atau file/secret manager — **bukan** dari
    `default.yaml`). Envelope in-app; DEK ter-wrap di `id.data_keys`. Master key ber-versi
    (`GOV_CRYPTO_MASTER_KEY_V2` dst) untuk rotasi. Tanpa dependensi eksternal — pemda tanpa
    HSM/Vault tetap jalan. Postur: keamanan turun ke perlindungan master key (ops L2), jauh
    di atas plaintext; ganti ke HSM/Vault = ganti driver, tanpa ubah kode.
  - `local` — dev/test saja, kunci statis dari config, tanpa versi — **ditolak bila
    `GOV_ENV=production`**. (Beda dari `static`: tak butuh secret source & tak mendukung rotasi.)
  - `vault` / `aws-kms` / `gcp-kms` / `bssn` / ... — di-plug saat pengadaan menentukan,
    tanpa ubah kode kripto.
- F6: **Custody = kebijakan per-tenant (ADR-010).** Resolver membaca `key_custody`
  (`platform|tenant`) dari tenant config/registry lalu memilih `KeyProvider` yang sesuai.
  `KeyRef` (tenant, purpose, custody) vendor-agnostik; pemetaan ke kunci nyata urusan driver.

## Kebutuhan non-fungsional
- Kunci mentah tak pernah tersimpan di DB tenant maupun di-log/trace.
- Overhead enkripsi tak boleh jadi bottleneck: cache DEK, hindari round-trip KMS per-baris.
- Rotasi KEK murah (re-wrap DEK). Rotasi DEK enkripsi lazy. Rotasi bidx = reindex (mahal,
  jarang) — didokumentasikan, bukan operasi rutin.

## Dependency
- port/crypto.go; KMS client (Vault Transit / ADR-010); config (driver, endpoint, kunci dev).
- Dipakai oleh: infra/db (enkripsi field transparan), core/audit (enkripsi diff sensitif).

## Anti-pattern
- Deterministic encryption untuk equality (bocorkan kesamaan) — pakai GCM + blind index.
- DEK ter-wrap di tenant DB. KEK/DEK/bidx-key di-log. Enkripsi dipanggil dari use case.
- Enkripsi kolom tanpa menutup jalur samping (audit/event/idempotency/staging/log) — ADR-009 §6.

## Acceptance criteria
- [ ] Roundtrip Encrypt→Decrypt mengembalikan plaintext identik; ciphertext dua panggilan
      atas plaintext sama BERBEDA (nonce acak).
- [ ] BlindIndex atas nilai sama (ternormalisasi) identik; atas nilai beda berbeda.
- [ ] tenantID berbeda → ciphertext tak bisa didekripsi lintas tenant (isolasi DEK).
- [ ] Rotasi key_version: ciphertext lama tetap terbaca; tulis baru pakai versi baru.
- [ ] Driver `local` menolak jalan saat `GOV_ENV=production`.
- [ ] Driver `static` menolak start bila `GOV_CRYPTO_MASTER_KEY` tak ada/tak valid (32-byte).
- [ ] Driver `static`: rotasi master key (V1→V2) — DEK lama tetap ter-unwrap, DEK baru pakai V2.
- [ ] `KeyProvider` baru bisa didaftarkan tanpa mengubah kode kripto (interface + Register).
- [ ] `key_custody` per-tenant diresolusi ke KeyProvider yang benar (platform vs tenant).
