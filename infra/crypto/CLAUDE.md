# infra/crypto — Crypto Adapter

Driven adapter: implementasi `port.CryptoPort` (ADR-009). Enkripsi field selektif
(AES-256-GCM) + blind index (HMAC-SHA256) untuk equality/UNIQUE. Envelope encryption
KEK→DEK dengan KMS (ADR-010). SENSITIF — perubahan menyentuh keamanan data pribadi.

## Status
IMPLEMENTED: driver `static` + `local`, custody `platform` (PR-3.8.2); sudah di-wire ke lapis
repository lewat `infra/db` (PR-3.8.3/3.8.4); ciphertext terikat BARIS (PR-3.8.9, ADR-016 —
format `0x02`, blob `0x01` ditolak `Decrypt`). Custody `tenant` + KMS eksternal = PR-3.8.8
(cukup `RegisterProvider` + satu `CustodyProvider`, tanpa ubah kode di sini).

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
- realm.go — `RealmCentral` + `WithCentralRealm` (ADR-017): sumbu partisi kunci adalah **realm**,
  bukan selalu tenant
- field_sealer.go — `FieldSealer`: kebijakan "satu field logis = dua kolom fisik" untuk repo yang
  ditulis TANGAN (identity, clone `gov.user_profiles`). Realm dipatri saat konstruksi. Ini
  SATU-SATUNYA tempat aturan "kosong → NULL", pengikatan baris wajib, dan pemeriksaan `PurposeOf`
  sebelum `Decrypt` ditulis — repo generik `infra/db` punya jalurnya sendiri (`fieldCrypto`) yang
  digerakkan `EntityDef`.

Driver KMS eksternal (vault/aws-kms/bssn) masuk sebagai file/paket baru yang memanggil
`RegisterProvider` — tanpa menyentuh file di atas.

## Key realm (ADR-017)
Sumbu partisi kunci (`id.data_keys.tenant_id`, `port.FieldRef.TenantID`) memuat identitas
**realm**, bukan selalu identitas tenant:

    realm tenant  = <tenant_id>          → data tenant; hierarki ADR-010 §2, tak berubah
    realm sentral = crypto.RealmCentral  → data identity + chain audit identity

- Realm sentral ada karena data identity memang tak punya tenant, dan yang mengunci pilihan
  itu `UNIQUE(nik_bidx)` yang berlaku global se-identity-DB — kunci bidx per-tenant membuat
  UNIQUE berhenti menangkap duplikat.
- Nilainya `_central` (garis bawah) SENGAJA: ia gagal `identity/domain.tenantIDRe` (`^[a-z]…`)
  sehingga tak bisa dipalsukan jadi tenant_id. Sentinel polos `"central"` ditolak justru karena
  ia nama tenant yang SAH.
- **Custody realm sentral = `platform`, invarian kode, tanpa menyentuh registry.** Identity DB
  adalah DB platform yang memuat data seluruh pemda; tak ada satu pemda yang berwenang memegang
  KEK-nya. Dipasang di `NewFromConfig` lewat `WithCentralRealm` — dan itu load-bearing:
  `DBCustodyResolver` fail-closed untuk identitas tak terdaftar, jadi tanpa dekorator itu realm
  sentral DITOLAK.
- Konstanta ini dipakai bersama jalur kripto identity DAN sentinel chain `id.audit_logs`. Kedua
  tempat HARUS memakai nilai yang sama: `core/audit.Reader` membangun `RowRef.TenantID` dari
  `entry.TenantID` untuk membuka diff.

## Konvensi khusus
- `purpose` memisahkan konteks kunci (mis. "nik" vs "no_rekening") tanpa ubah port.
- **AAD mengikat (tenant, purpose, key_version, record_id)** — ADR-016. tenant & record_id
  disuplai pemanggil (`port.FieldRef`/`port.RowRef`); purpose & versi dibaca dari blob.
  Yang menegakkan justru komponen dari pemanggil: blob yang dipindah diminta dibuka dengan
  koordinat tujuan. `record_id` TIDAK disimpan di blob — kalau disimpan, blob yang dipindah
  membawa serta "bukti" identitasnya sendiri dan pengikatan jadi tak berarti.
- Komponen AAD ditulis ber-length-prefix (`uint32(len) || bytes`), bukan digabung dengan
  pemisah: `record_id` tak selamanya UUID, dan pemisah polos membuat dua koordinat berbeda
  bisa menghasilkan AAD yang sama.
- `BlindIndex` sengaja TIDAK menerima `FieldRef` — ia wajib row-independent (ADR-016 §3).
- Format ciphertext `0x01` (pra-ADR-016) masih DIKENALI parser (agar `PurposeOf` menjawab &
  jalur baca audit menampilkan penanda, bukan blob mentah) tapi DITOLAK `Decrypt` dengan
  pesan yang menyebut re-enkripsi. Jangan "melonggarkan demi kompatibilitas": menerima v1 =
  menerima ciphertext tak terikat baris.
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
- Menaruh `record_id` ke dalam BLOB alih-alih AAD "supaya self-describing" — itu tidak
  mengamankan apa pun (ADR-016 §Alternatif), hanya memindahkan pemeriksaan ke lapis
  aplikasi yang bisa dilangkahi jalur tulis mana pun yang lupa memeriksanya.
- Menyimpan DEK ter-wrap di tenant DB (dump membuka jalan) — DEK di sentral/KMS.
- Menyisipkan baris tenant palsu `_central` ke `id.tenant_registry` "supaya custody resolver
  tak perlu dekorator". Itu mengubah custody yang TIDAK bisa dinegosiasikan menjadi baris data
  yang bisa di-`UPDATE` (`SET key_custody='tenant'` → identity DB tak terbaca, gejalanya baru
  muncul saat kripto dipakai) dan membocorkan tenant palsu ke `TenantRegistry.List` &
  `pamongctl tenant provision`. Ditolak eksplisit di ADR-017 §Alternatif.
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
- PRD.md, port/crypto.go, docs/adr/009-*, docs/adr/010-*, docs/adr/015-* (pengikatan
  kolom), docs/adr/016-* (pengikatan baris), ADR-002 (audit diff)
