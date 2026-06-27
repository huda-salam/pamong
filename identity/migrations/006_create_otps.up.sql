-- OTP ephemeral untuk login persona citizen jalur OTP (email/no_hp tanpa password). PR-2.4.4.
-- Di identity DB sentral (schema id). Menempel pada credential; code_hash = HASH bcrypt (BUKAN
-- plaintext). Berumur pendek (expires_at), sekali pakai (consumed_at), percobaan dibatasi
-- (attempts). Token pendek → baris cukup hidup sampai expires_at lalu boleh dipurge (lazy /
-- scheduler menyusul, cermin id.revoked_tokens). Additive / backward-compatible.

CREATE TABLE id.otps (
    id            UUID PRIMARY KEY,
    credential_id UUID NOT NULL REFERENCES id.credentials(id) ON DELETE CASCADE,
    code_hash     VARCHAR(255) NOT NULL,        -- bcrypt; tak pernah plaintext
    expires_at    TIMESTAMPTZ NOT NULL,         -- TTL pendek (default 5 menit dari penerbitan)
    consumed_at   TIMESTAMPTZ,                  -- non-null = sudah dipakai/dihanguskan
    attempts      INT NOT NULL DEFAULT 0,       -- verifikasi gagal; cap di domain.MaxOTPAttempts
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Verifikasi mencari OTP TERBARU per credential (ORDER BY created_at DESC LIMIT 1).
CREATE INDEX idx_otps_credential_created ON id.otps (credential_id, created_at DESC);
