-- Idempotency keys (framework-level data integrity, CLAUDE.md §Data integrity). Menyimpan
-- respons request mutasi ber-Idempotency-Key agar retry/duplikat (key sama) mengembalikan
-- respons yang sama tanpa efek ganda. Ditegakkan di middleware gateway, bukan use case.
--
-- Yang tersimpan hanya DATA (fingerprint + respons + masa berlaku) — bukan logika.
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.idempotency_keys (
    -- Di-scope ke PRINCIPAL: satu user tak boleh membaca/menimpa respons user lain lewat
    -- menebak/menggunakan-ulang nilai key yang sama (PK gabungan person_id + key).
    person_id   UUID        NOT NULL,
    key         TEXT        NOT NULL,
    -- hash(method + path + body): deteksi key dipakai-ulang untuk request BERBEDA (→ 422).
    fingerprint TEXT        NOT NULL,
    -- status & response NULL selama reservasi masih pending (handler belum selesai).
    status      INT,
    response    BYTEA,
    completed   BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Reservasi pending berumur pendek (retry cepat pulih bila request crash); entri completed
    -- diperpanjang ke replay window (mis. 24 jam) saat Complete. Baris kedaluwarsa boleh
    -- diambil-alih reservasi baru (lihat store Reserve).
    expires_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (person_id, key)
);

-- Mendukung pembersihan / seleksi baris kedaluwarsa.
CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON gov.idempotency_keys (expires_at);
