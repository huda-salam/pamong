-- Daftar token internal yang DICABUT (revocation), di identity DB sentral (schema id). PR-2.4.1.
-- Verifikasi token menolak bila jti ada di sini. Token berumur pendek → baris cukup disimpan
-- sampai expires_at lalu boleh dipurge (lazy/scheduler menyusul). Denylist per-jti; "cabut semua
-- token person" (epoch tokens_valid_after, mis. saat central role dicabut) menyusul saat event
-- di-wire (DEFERRED Phase-2.4). Additive / backward-compatible.

CREATE TABLE id.revoked_tokens (
    jti         UUID PRIMARY KEY,
    person_id   UUID NOT NULL REFERENCES id.persons(id),
    expires_at  TIMESTAMPTZ NOT NULL,   -- = exp token; batas hidup entri (boleh dipurge setelahnya)
    revoked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason      TEXT
);
CREATE INDEX idx_revoked_tokens_expires ON id.revoked_tokens (expires_at);
