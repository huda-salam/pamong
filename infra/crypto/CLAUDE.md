# infra/crypto — Crypto Adapter

Driven adapter: implementasi `port.CryptoPort` (ADR-009). Enkripsi field selektif
(AES-256-GCM) + blind index (HMAC-SHA256) untuk equality/UNIQUE. Envelope encryption
KEK→DEK dengan KMS (ADR-010). SENSITIF — perubahan menyentuh keamanan data pribadi.

## Status
IMPLEMENTED (PR-3.8.2): driver `static` + `local`, custody `platform`. BELUM di-wire ke lapis
repository — enkripsi transparan dari `FieldDef.Class` adalah PR-3.8.3, jadi belum ada kolom
produksi terenkripsi. Custody `tenant` + KMS eksternal = PR-3.8.8 (cukup `RegisterProvider` +
satu `CustodyProvider`, tanpa ubah kode di sini).

## Bergantung pada
- port/crypto.go; core/config (driver & master key); port.DBConn ke IDENTITY DB
  (id.data_keys + id.tenant_registry.key_custody); stdlib crypto; pgx (deteksi no-rows)

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

## File kunci
- crypto.go — entry: `Service` (impl CryptoPort), `New`/`NewFromConfig`, format ciphertext,
  normalisasi nilai blind index
- provider.go — `KeyProvider` + `KeyRef` + registry driver KMS (`RegisterProvider`), `Custody`,
  `KeyKind`
- kek.go — `kekWrapper`: envelope in-app AES-256-GCM untuk driver bawaan; AAD mengikat blob
  ke KeyRef; aturan "versi tertinggi = aktif"
- drivers.go — driver `static` (master KEK ber-versi dari secret) & `local` (dev/test)
- dek_store.go — `DEKStore` + `DBDEKStore` di atas id.data_keys (identity DB, sentral)
- envelope.go — `keyManager`: hierarki KEK→DEK, pembuatan kunci saat pemakaian pertama,
  cache DEK ter-decrypt (in-proc, TTL)
- custody.go — `CustodyResolver`: baca id.tenant_registry.key_custody (cache TTL) → KeyProvider

Driver KMS eksternal (vault/aws-kms/bssn) masuk sebagai file/paket baru yang memanggil
`RegisterProvider` — tanpa menyentuh file di atas.

## Konvensi khusus
- `purpose` memisahkan konteks kunci (mis. "nik" vs "no_rekening") tanpa ubah port.
- Kunci blind-index TERPISAH dari kunci enkripsi (`kind` di id.data_keys: enc vs bidx), BUKAN
  turunan satu DEK — kalau diturunkan, rotasi kunci enkripsi ikut memaksa reindex.
  Rotasi bidx = reindex seluruh baris (mahal, hanya saat kompromi).
- Ciphertext membawa key_version → dekripsi lama tetap jalan saat rotasi (lazy re-encrypt).
- Kunci dibuat OTOMATIS saat purpose dipakai pertama kali (bukan langkah provisioning manual
  yang bisa terlewat). Balapan dijaga unique index parsial `uq_data_keys_active` — pemanggil
  yang kalah memakai baris pemenang, tak pernah ada dua kunci aktif.
- Normalisasi nilai blind index (trim; case-fold untuk purpose tertentu spt `email`) adalah
  tabel KEBIJAKAN FRAMEWORK di crypto.go, bukan pilihan per-modul. Salah normalisasi = UNIQUE
  lolos duplikat atau lookup gagal.
- Custody yang tak punya KeyProvider terdaftar DITOLAK (`ErrCustodyUnsupported`) — jangan
  tambahkan fallback ke platform: itu memberi jaminan kedaulatan kunci yang tidak benar.
- Unwrap memakai provider sesuai kolom `custody` BARIS ITU, bukan custody tenant saat ini —
  supaya data lama tetap terbaca setelah custody berpindah.

## Pitfall umum
- Deterministic encryption untuk equality (membocorkan kesamaan nilai) — pakai GCM + blind
  index, bukan itu.
- Menyimpan DEK ter-wrap di tenant DB (dump membuka jalan) — DEK di sentral/KMS.
- Lupa menutup jalur kebocoran samping (audit diff, event, idempotency, log) — enkripsi
  kolom saja = teater keamanan (ADR-009 §6).

## Test
- Unit (store DEK di memori, driver local/static): roundtrip enc/dec; ciphertext non-deterministik;
  blind index deterministik & terpisah per tenant/purpose; ciphertext rusak/lintas-tenant ditolak;
  rotasi DEK & master KEK; blob DEK terikat KeyRef; registry driver; custody ditolak lantang.
- Integration (`-tags=integration`, Postgres nyata): id.data_keys insert/baca/versi, balapan
  insert → satu kunci aktif, end-to-end Service dengan registry & custody dari DB, down migration.
- `go test ./infra/crypto/... -race` dan
  `PAMONG_TEST_DB_DSN=... go test ./infra/crypto/... -tags=integration -p 1`

## Rujukan
- PRD.md, port/crypto.go, docs/adr/009-*, docs/adr/010-*, ADR-002 (audit diff)
