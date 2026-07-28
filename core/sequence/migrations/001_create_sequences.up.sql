-- Sequence counters (framework-level). Sumber nomor ber-urut ATOMIK per-tenant untuk
-- dokumen bisnis (nomor agenda surat, nomor SPM, dst). Ditegakkan di driven adapter
-- (infra/sequence) di atas port.SequenceGenerator, bukan use case.
--
-- Isolasi antar-tenant = DB tenant itu sendiri (DB-per-tenant, CLAUDE.md): tabel ini hidup
-- di dalam setiap tenant DB, jadi tak butuh kolom tenant_id.
--
-- Yang tersimpan hanya DATA (identitas sequence + nilai terakhir) — bukan logika.
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.sequences (
    -- Identitas sequence = pola nomor yang diminta caller (mis. "{tahun}/AG/{nomor:5}").
    -- Dua pola berbeda → dua penghitung berbeda; caller yang sama selalu memakai pola sama.
    name       TEXT   NOT NULL,
    -- Reset per tahun fiskal bersifat intrinsik: tahun jadi bagian PK, sehingga tahun baru
    -- memulai penghitung dari nol tanpa proses reset terpisah.
    tahun      INT    NOT NULL,
    -- Nilai terakhir yang sudah diberikan. Increment ATOMIK lewat UPDATE ... RETURNING.
    current    BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (name, tahun)
);
