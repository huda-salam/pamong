-- Enkripsi pengenal identity + pemindahan UNIQUE ke blind index (ADR-009 §2, ADR-017).
-- PR-3.8.6.
--
-- Satu field logis terenkripsi = DUA kolom fisik:
--   {f}_enc  BYTEA — AES-256-GCM, nonce acak per-nilai (dua nilai sama → ciphertext beda,
--                    karena itu kolom ini TAK BISA dipakai equality maupun UNIQUE)
--   {f}_bidx BYTEA — HMAC-SHA256 deterministik atas nilai ternormalisasi; INILAH yang
--                    menopang lookup equality & UNIQUE (ADR-009 §2)
--
-- Kunci realm SENTRAL (`_central`, ADR-017): data identity tak punya tenant, dan
-- UNIQUE(nik) berlaku global se-identity-DB — kunci blind index per-tenant akan membuat
-- NIK yang sama menghasilkan bidx berbeda, sehingga UNIQUE berhenti menangkap duplikat.
--
-- BUKAN migrasi additive, dan itu disengaja. Kolom plaintext DI-DROP, bukan dibiarkan
-- berdampingan: meninggalkannya berarti nilai mentah tetap ada di dump — persis kebocoran
-- yang hendak ditutup. Backfill tidak ada karena tidak ada satu baris pun untuk dibackfill:
-- diverifikasi 1 Agu 2026 bahwa identity DB kosong di SELURUH environment (tak ada
-- deployment staging/production; dev & CI tanpa schema `id`). Window ini tidak terulang —
-- sesudah ada data produksi, perubahan yang sama menuntut pipeline re-enkripsi bertahap
-- (tambah kolom → backfill → tukar constraint → drop) dengan window kompatibilitas dua rilis.

-- === id.persons ===
-- nik: pengenal utama (class personal_id). UNIQUE pindah dari nik ke nik_bidx.
-- no_hp & email: kontak (class personal_id). Belum ada query equality ke keduanya hari ini
-- (lookup kontak untuk login lewat id.credentials), tapi kolom _bidx tetap dibuat sekarang:
-- menambahkannya kelak berarti reindex seluruh baris — alasan penjadwalan yang sama dengan
-- migrasi ini sendiri.
ALTER TABLE id.persons
    DROP COLUMN nik,
    DROP COLUMN no_hp,
    DROP COLUMN email,
    ADD COLUMN nik_enc    BYTEA NOT NULL,
    ADD COLUMN nik_bidx   BYTEA NOT NULL,
    ADD COLUMN no_hp_enc  BYTEA,
    ADD COLUMN no_hp_bidx BYTEA,
    ADD COLUMN email_enc  BYTEA,
    ADD COLUMN email_bidx BYTEA;

-- Menggantikan UNIQUE lama pada kolom nik plaintext.
CREATE UNIQUE INDEX uq_persons_nik_bidx ON id.persons (nik_bidx);

-- no_hp & email TIDAK unik (dua orang boleh berbagi nomor rumah tangga / email keluarga) —
-- sama seperti sebelum migrasi ini. Index non-unik agar pencarian kelak tidak seq-scan.
CREATE INDEX idx_persons_no_hp_bidx ON id.persons (no_hp_bidx);
CREATE INDEX idx_persons_email_bidx ON id.persons (email_bidx);

-- === id.employments ===
-- nip NULL untuk non-ASN (banyak baris NULL diizinkan UNIQUE Postgres) — perilaku itu
-- dipertahankan apa adanya di nip_bidx.
--
-- CHECK (status='asn' ⇒ nip NOT NULL) ikut pindah ke nip_enc. Ia harus di-DROP lebih dulu:
-- constraint tanpa nama eksplisit di migrasi 001 mendapat nama generated employments_check.
ALTER TABLE id.employments DROP CONSTRAINT employments_check;

ALTER TABLE id.employments
    DROP COLUMN nip,
    ADD COLUMN nip_enc  BYTEA,
    ADD COLUMN nip_bidx BYTEA,
    ADD CONSTRAINT employments_nip_status_check CHECK (
        (status = 'asn'     AND nip_enc IS NOT NULL AND nip_bidx IS NOT NULL) OR
        (status = 'non_asn' AND nip_enc IS NULL     AND nip_bidx IS NULL)
    );

CREATE UNIQUE INDEX uq_employments_nip_bidx ON id.employments (nip_bidx);

-- === id.credentials ===
-- cred_type TETAP plaintext: ia jenis kredensial, bukan pengenal orang. Membiarkannya
-- terbuka itulah yang membuat UNIQUE (cred_type, cred_value_bidx) tetap menegakkan
-- keunikan per-tipe dan FindByTypeValue tetap satu query.
ALTER TABLE id.credentials
    DROP COLUMN cred_value,
    ADD COLUMN cred_value_enc  BYTEA NOT NULL,
    ADD COLUMN cred_value_bidx BYTEA NOT NULL;

-- Menggantikan UNIQUE (cred_type, cred_value) lama.
CREATE UNIQUE INDEX uq_credentials_type_value_bidx
    ON id.credentials (cred_type, cred_value_bidx);
