-- DEK ter-wrap untuk enkripsi field selektif (ADR-009/010). PR-3.8.2.
--
-- SENTRAL (identity DB), BUKAN tenant DB — ini keputusan keamanan: dump satu tenant DB
-- berisi ciphertext saja, tak memuat kunci apa pun untuk membukanya (ADR-010 §2).
--
-- Yang tersimpan HANYA DEK yang sudah dibungkus KEK (wrapped_dek). KEK sendiri hidup di
-- KeyProvider (KMS/master key ops) dan tidak pernah masuk DB. Kunci mentah juga tidak
-- pernah di-log (infra/crypto/CLAUDE.md).
--
-- Granularitas kunci: (tenant_id, purpose, kind).
--   tenant_id — isolasi per-tenant wajib (DB-per-tenant ADR-004 + Tier 3): satu tenant
--               bocor tak membuka tenant lain.
--   purpose   — konteks kunci ('nik', 'no_rekening', ...) membatasi blast radius.
--   kind      — 'enc' (AES-256-GCM) vs 'bidx' (HMAC blind index). SENGAJA kunci terpisah,
--               bukan turunan satu DEK: rotasi kunci enkripsi murah & lazy, sedangkan
--               rotasi kunci blind index memaksa reindex seluruh baris (ADR-009 §2).
--               Menurunkan keduanya dari satu DEK akan menyeret reindex mahal setiap kali
--               kunci enkripsi dirotasi.

CREATE TABLE id.data_keys (
    tenant_id   VARCHAR(100) NOT NULL,
    purpose     VARCHAR(50)  NOT NULL,
    kind        VARCHAR(10)  NOT NULL CHECK (kind IN ('enc','bidx')),
    key_version INTEGER      NOT NULL CHECK (key_version > 0),
    -- DEK ter-wrap. Format ditentukan driver KeyProvider (self-describing, membawa versi
    -- KEK) sehingga rotasi KEK = re-wrap kolom ini tanpa menyentuh data.
    wrapped_dek BYTEA        NOT NULL,
    -- Driver & custody yang MEMBUNGKUS versi ini — diagnosa/audit saat custody berpindah
    -- atau KMS diganti (baris lama tetap bisa dibuka driver yang benar).
    kek_driver  VARCHAR(30)  NOT NULL,
    custody     VARCHAR(10)  NOT NULL DEFAULT 'platform' CHECK (custody IN ('platform','tenant')),
    -- Versi aktif = yang dipakai untuk TULIS. Versi non-aktif tetap wajib ada agar
    -- ciphertext lama tetap terbaca (lazy re-encrypt, ADR-010 §4).
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    rotated_at  TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, purpose, kind, key_version)
);

-- Tepat satu versi aktif per (tenant, purpose, kind): tanpa ini dua proses bisa menulis
-- dengan versi berbeda dan "versi aktif" jadi ambigu.
CREATE UNIQUE INDEX uq_data_keys_active
    ON id.data_keys (tenant_id, purpose, kind)
    WHERE is_active;
