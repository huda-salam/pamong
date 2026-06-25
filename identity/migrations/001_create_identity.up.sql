-- Migrasi identity DB (gov_identity, schema id). Berjalan terhadap DB SENTRAL,
-- terpisah dari tenant DB. Hanya additive (backward-compatible).
-- PR-2.1.1: person (anchor NIK) + employment (opsional, NIP untuk ASN) + credential.

CREATE SCHEMA IF NOT EXISTS id;

-- Master identitas — satu baris per manusia, anchor di NIK.
CREATE TABLE id.persons (
    id           UUID PRIMARY KEY,
    nik          VARCHAR(16) UNIQUE NOT NULL,
    nama_lengkap VARCHAR(255) NOT NULL,
    tgl_lahir    DATE,
    no_hp        VARCHAR(15),
    email        VARCHAR(255),
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Employment — relasi kepegawaian, opsional, bisa >1 sepanjang waktu.
-- Constraint: status=asn ⇒ nip NOT NULL; status=non_asn ⇒ nip NULL.
CREATE TABLE id.employments (
    id            UUID PRIMARY KEY,
    person_id     UUID NOT NULL REFERENCES id.persons(id),
    status        VARCHAR(10) NOT NULL CHECK (status IN ('asn','non_asn')),
    nip           VARCHAR(18) UNIQUE,
    instansi_asal VARCHAR(255),
    is_active     BOOLEAN NOT NULL DEFAULT true,
    valid_from    TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status = 'asn' AND nip IS NOT NULL) OR (status = 'non_asn' AND nip IS NULL))
);
CREATE INDEX idx_employments_person ON id.employments (person_id);

-- Credential login — banyak per person, semua resolve ke person yang sama.
CREATE TABLE id.credentials (
    id           UUID PRIMARY KEY,
    person_id    UUID NOT NULL REFERENCES id.persons(id),
    cred_type    VARCHAR(20) NOT NULL CHECK (cred_type IN ('nip','nik','email','no_hp','oauth')),
    cred_value   VARCHAR(255) NOT NULL,
    secret_hash  VARCHAR(255),
    is_primary   BOOLEAN NOT NULL DEFAULT false,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cred_type, cred_value)
);
CREATE INDEX idx_credentials_person ON id.credentials (person_id);
