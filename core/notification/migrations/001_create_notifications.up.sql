-- Notification Hub: template per-tenant/locale, kotak masuk in-app, jejak pengiriman (PR-3.6.1).
-- Yang tersimpan di DB hanya KONTEN (template teks) & DATA (item/jejak) — BUKAN logika. Channel
-- adalah kode Go ter-registry (notification.ChannelRegistry); routing peran→orang ada di
-- core/permission (PR-3.6.2). Menutup vektor "kode arbitrary di DB" (CLAUDE.md §Fleksibilitas).
CREATE SCHEMA IF NOT EXISTS gov;

-- Template notifikasi. tenant_id kosong = template GLOBAL (default framework/modul); baris
-- tenant-spesifik meng-override untuk tenant tsb. Pemilihan "paling cocok" (tenant > global,
-- locale sama > default) dilakukan di TemplateEngine — tabel hanya menyimpan kandidat.
CREATE TABLE IF NOT EXISTS gov.notification_templates (
    tenant_id  TEXT        NOT NULL DEFAULT '',  -- '' = global default
    key        TEXT        NOT NULL,             -- {modul}.{kejadian}
    locale     TEXT        NOT NULL DEFAULT 'id',
    subject    TEXT        NOT NULL DEFAULT '',
    body       TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_notif_template UNIQUE (tenant_id, key, locale)
);

CREATE INDEX IF NOT EXISTS idx_notif_template_lookup
    ON gov.notification_templates (tenant_id, key);

-- Kotak masuk in-app per penerima. Channel in_app menulis ke sini; UI penerima membaca
-- lewat use case (bukan query langsung).
CREATE TABLE IF NOT EXISTS gov.notification_inapp (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT        NOT NULL DEFAULT '',
    person_id    UUID        NOT NULL,
    template_key TEXT        NOT NULL DEFAULT '',
    subject      TEXT        NOT NULL DEFAULT '',
    body         TEXT        NOT NULL,
    is_read      BOOLEAN     NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Ambil kotak masuk penerima terbaru dulu.
CREATE INDEX IF NOT EXISTS idx_notif_inapp_recipient
    ON gov.notification_inapp (tenant_id, person_id, created_at DESC);

-- Jejak pengiriman: satu baris per upaya kirim per channel (delivered/failed/read) untuk
-- audit "kenapa notif tak sampai".
CREATE TABLE IF NOT EXISTS gov.notification_deliveries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT        NOT NULL DEFAULT '',
    person_id    UUID        NOT NULL,
    channel      TEXT        NOT NULL,
    template_key TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL,           -- 'delivered' | 'failed' | 'read'
    error        TEXT        NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notif_delivery_recipient
    ON gov.notification_deliveries (tenant_id, person_id, delivered_at DESC);
