-- Down WAJIB ada (linter: migration-needs-down). Urutan terbalik dari up.
--
-- PERINGATAN: down ini mengembalikan BENTUK skema, bukan DATA. Kolom plaintext dibuat
-- kembali kosong; nilai yang ada di kolom _enc TIDAK dikembalikan — memulihkannya menuntut
-- kunci dari KeyProvider, yang bukan urusan migrasi SQL. Menjalankan down pada DB berisi
-- data = kehilangan seluruh pengenal (dan, karena NOT NULL, gagal sejak awal pada
-- id.persons/id.credentials yang tidak kosong). Aman hanya selama identity DB kosong,
-- yaitu kondisi yang membuat migrasi up ini murah sejak awal (ADR-017 §Konsekuensi).

-- === id.credentials ===
DROP INDEX IF EXISTS id.uq_credentials_type_value_bidx;
ALTER TABLE id.credentials
    DROP COLUMN cred_value_enc,
    DROP COLUMN cred_value_bidx,
    ADD COLUMN cred_value VARCHAR(255) NOT NULL,
    ADD UNIQUE (cred_type, cred_value);

-- === id.employments ===
DROP INDEX IF EXISTS id.uq_employments_nip_bidx;
ALTER TABLE id.employments DROP CONSTRAINT employments_nip_status_check;
ALTER TABLE id.employments
    DROP COLUMN nip_enc,
    DROP COLUMN nip_bidx,
    ADD COLUMN nip VARCHAR(18) UNIQUE,
    ADD CHECK ((status = 'asn' AND nip IS NOT NULL) OR (status = 'non_asn' AND nip IS NULL));

-- === id.persons ===
DROP INDEX IF EXISTS id.uq_persons_nik_bidx;
DROP INDEX IF EXISTS id.idx_persons_no_hp_bidx;
DROP INDEX IF EXISTS id.idx_persons_email_bidx;
ALTER TABLE id.persons
    DROP COLUMN nik_enc,
    DROP COLUMN nik_bidx,
    DROP COLUMN no_hp_enc,
    DROP COLUMN no_hp_bidx,
    DROP COLUMN email_enc,
    DROP COLUMN email_bidx,
    ADD COLUMN nik VARCHAR(16) UNIQUE NOT NULL,
    ADD COLUMN no_hp VARCHAR(15),
    ADD COLUMN email VARCHAR(255);
