-- Instance workflow: satu baris per jalannya alur untuk satu entitas bisnis (PR-W4a, ADR-022).
--
-- Sampai migrasi ini, engine sudah lengkap tapi TAK ADA tempat menyimpan instance — sehingga
-- transisi hanya mungkin dalam satu request (instance hidup di memori pemanggil) dan guard race
-- eskalasi SLA tak punya sumber data. Tabel ini yang membuat alur berhari-hari benar-benar bisa
-- dijalankan.
--
-- definition_version DIKUNCI saat start: perubahan definisi setelahnya tidak mengubah instance
-- yang sedang berjalan (PRD workflow F1/F7).
--
-- role_bindings adalah SALINAN BEKU pilihan tenant saat StartFromTemplate (ADR-012) — sengaja
-- disimpan pada instance, bukan dibaca ulang dari gov.tenant_workflow_configs tiap transisi.
--
-- history disimpan sebagai JSONB append-only pada baris yang sama, bukan tabel terpisah:
-- riwayat selalu dibaca UTUH bersama instance-nya dan tak pernah di-query lintas instance.
-- Immutabilitasnya dijaga jalur tulis (hanya append) + gov.audit_logs, bukan constraint DDL.
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.workflow_instances (
    id                 UUID PRIMARY KEY,
    tenant_id          TEXT        NOT NULL,
    definition_id      TEXT        NOT NULL,
    definition_version INT         NOT NULL,
    entity_id          UUID        NOT NULL,
    current_state      TEXT        NOT NULL,
    role_bindings      JSONB       NOT NULL DEFAULT '{}',
    history            JSONB       NOT NULL DEFAULT '[]',
    version            INT         NOT NULL DEFAULT 0,  -- optimistic locking (CLAUDE.md §Data integrity)
    started_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- SATU alur per (definisi, entitas). Ini keunikan, bukan sekadar index pencarian: tanpanya siapa
-- pun yang boleh memulai alur bisa membuat instance BARU di initial_state untuk dokumen yang
-- alurnya sudah selesai, lalu menjalankan lagi seluruh action-nya — mendisposisi ulang surat yang
-- sudah ditutup, berkali-kali, tanpa satu pun gerbang yang dilanggar.
--
-- Konsekuensi yang disengaja: alur yang sama tak bisa dijalankan ULANG atas entitas yang sama.
-- Perulangan yang sah dimodelkan DI DALAM definisi (self-loop terkontrol, seperti disposisi
-- berjenjang di disposisi.yaml), bukan dengan memulai instance kedua.
CREATE UNIQUE INDEX IF NOT EXISTS uq_wfinst_entity_definition
    ON gov.workflow_instances (definition_id, entity_id);

-- Lookup instance yang masih berjalan di satu state (mis. papan kerja "menunggu tindak lanjut").
CREATE INDEX IF NOT EXISTS idx_wfinst_state
    ON gov.workflow_instances (definition_id, current_state);
