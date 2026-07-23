// Package notification menyediakan driven adapter Postgres untuk port-port Notification Hub
// (TemplateStore, InAppInbox, DeliveryRecorder). Seluruh kode yang menyentuh pgx HANYA ada di
// infra — core/notification tak pernah mengimport infra (linter: domain-no-infra-import).
package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/infra/db"
)

// notificationDDL membuat schema gov + tabel template/inbox/jejak bila belum ada. Identik
// dengan migration 001 — dipakai EnsureSchema untuk bootstrap langsung (pola AuditRepo/DBStore).
const notificationDDL = `
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.notification_templates (
    tenant_id  TEXT        NOT NULL DEFAULT '',
    key        TEXT        NOT NULL,
    locale     TEXT        NOT NULL DEFAULT 'id',
    subject    TEXT        NOT NULL DEFAULT '',
    body       TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_notif_template UNIQUE (tenant_id, key, locale)
);
CREATE INDEX IF NOT EXISTS idx_notif_template_lookup
    ON gov.notification_templates (tenant_id, key);

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
CREATE INDEX IF NOT EXISTS idx_notif_inapp_recipient
    ON gov.notification_inapp (tenant_id, person_id, created_at DESC);

CREATE TABLE IF NOT EXISTS gov.notification_deliveries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT        NOT NULL DEFAULT '',
    person_id    UUID        NOT NULL,
    channel      TEXT        NOT NULL,
    template_key TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL,
    error        TEXT        NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notif_delivery_recipient
    ON gov.notification_deliveries (tenant_id, person_id, delivered_at DESC);`

// EnsureSchema membuat schema gov & seluruh tabel notifikasi bila belum ada. Idempoten.
// Dipanggil sekali dari satu store cukup untuk ketiga store (tabel dibuat bersama).
func EnsureSchema(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, notificationDDL)
	return err
}

// --- DBTemplateStore ---

// DBTemplateStore mengimplementasi coreNotif.TemplateStore di atas Postgres. Pemilihan
// template tetap di core (TemplateEngine) — store hanya mengambil kandidat & meng-upsert.
type DBTemplateStore struct {
	pool *db.Pool
}

// NewDBTemplateStore membuat store. Panggil EnsureSchema sebelum dipakai.
func NewDBTemplateStore(pool *db.Pool) *DBTemplateStore { return &DBTemplateStore{pool: pool} }

var _ coreNotif.TemplateStore = (*DBTemplateStore)(nil)

// Candidates mengembalikan template tenant-spesifik + global untuk key (lintas locale).
func (s *DBTemplateStore) Candidates(ctx context.Context, tenantID, key string) ([]coreNotif.Template, error) {
	// gov:raw-ok reason=template-candidates query=notification-template-candidates
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id, key, locale, subject, body
		FROM gov.notification_templates
		WHERE key = $1 AND tenant_id IN ('', $2)`,
		key, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query notification_templates: %w", err)
	}
	defer rows.Close()

	var out []coreNotif.Template
	for rows.Next() {
		var t coreNotif.Template
		if err := rows.Scan(&t.TenantID, &t.Key, &t.Locale, &t.Subject, &t.Body); err != nil {
			return nil, fmt.Errorf("scan notification_template: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi notification_templates: %w", err)
	}
	return out, nil
}

// Upsert menyimpan/menimpa template untuk (tenant, key, locale). Locale kosong → DefaultLocale.
func (s *DBTemplateStore) Upsert(ctx context.Context, t coreNotif.Template) error {
	if t.Key == "" || t.Body == "" {
		return coreNotif.ErrInvalidTemplate("key dan body template wajib diisi")
	}
	// gov:raw-ok reason=upsert-template query=notification-template-upsert
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.notification_templates (tenant_id, key, locale, subject, body, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT ON CONSTRAINT uq_notif_template
		DO UPDATE SET subject = EXCLUDED.subject, body = EXCLUDED.body, updated_at = now()`,
		t.TenantID, t.Key, t.LocaleOrDefault(), t.Subject, t.Body)
	if err != nil {
		return fmt.Errorf("upsert notification_template: %w", err)
	}
	return nil
}

// --- DBInAppInbox ---

// DBInAppInbox mengimplementasi coreNotif.InAppInbox di atas Postgres.
type DBInAppInbox struct {
	pool *db.Pool
}

// NewDBInAppInbox membuat inbox store. Panggil EnsureSchema sebelum dipakai.
func NewDBInAppInbox(pool *db.Pool) *DBInAppInbox { return &DBInAppInbox{pool: pool} }

var _ coreNotif.InAppInbox = (*DBInAppInbox)(nil)

// Append menyisipkan satu item ke kotak masuk, mengembalikan ID yang dibuat DB.
func (b *DBInAppInbox) Append(ctx context.Context, item coreNotif.InAppItem) (string, error) {
	// gov:raw-ok reason=insert-inapp query=notification-inapp-append
	var id uuid.UUID
	err := b.pool.QueryRow(ctx, `
		INSERT INTO gov.notification_inapp (tenant_id, person_id, template_key, subject, body)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		item.TenantID, item.PersonID, item.TemplateKey, item.Subject, item.Body,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("append notification_inapp: %w", err)
	}
	return id.String(), nil
}

// List mengembalikan item milik (tenant, person), terbaru dulu, maksimal limit (0 = semua).
func (b *DBInAppInbox) List(ctx context.Context, tenantID, personID string, limit int) ([]coreNotif.InAppItem, error) {
	pid, err := uuid.Parse(personID)
	if err != nil {
		return nil, coreNotif.ErrInvalidPersonID(personID)
	}
	// limit 0 → NULL (tanpa batas); Postgres LIMIT NULL = semua baris.
	var limitArg any
	if limit > 0 {
		limitArg = limit
	}
	// gov:raw-ok reason=list-inapp query=notification-inapp-list
	rows, err := b.pool.Query(ctx, `
		SELECT id, tenant_id, person_id, template_key, subject, body, is_read, created_at
		FROM gov.notification_inapp
		WHERE tenant_id = $1 AND person_id = $2
		ORDER BY created_at DESC
		LIMIT $3`,
		tenantID, pid, limitArg)
	if err != nil {
		return nil, fmt.Errorf("query notification_inapp: %w", err)
	}
	defer rows.Close()

	var out []coreNotif.InAppItem
	for rows.Next() {
		var (
			it coreNotif.InAppItem
			id uuid.UUID
		)
		if err := rows.Scan(&id, &it.TenantID, &it.PersonID, &it.TemplateKey,
			&it.Subject, &it.Body, &it.Read, &it.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification_inapp: %w", err)
		}
		it.ID = id.String()
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi notification_inapp: %w", err)
	}
	return out, nil
}

// --- DBDeliveryRecorder ---

// DBDeliveryRecorder mengimplementasi coreNotif.DeliveryRecorder di atas Postgres.
type DBDeliveryRecorder struct {
	pool *db.Pool
}

// NewDBDeliveryRecorder membuat recorder. Panggil EnsureSchema sebelum dipakai.
func NewDBDeliveryRecorder(pool *db.Pool) *DBDeliveryRecorder { return &DBDeliveryRecorder{pool: pool} }

var _ coreNotif.DeliveryRecorder = (*DBDeliveryRecorder)(nil)

// Record menyimpan satu jejak pengiriman. delivered_at nol → now() (dihitung di sini agar
// query tunggal, konsisten dengan MemoryDeliveryRecorder yang juga mengisi At bila kosong).
func (r *DBDeliveryRecorder) Record(ctx context.Context, rec coreNotif.DeliveryRecord) error {
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	// gov:raw-ok reason=insert-delivery query=notification-delivery-record
	_, err := r.pool.Exec(ctx, `
		INSERT INTO gov.notification_deliveries
		    (tenant_id, person_id, channel, template_key, status, error, delivered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rec.TenantID, rec.PersonID, rec.Channel, rec.TemplateKey, string(rec.Status), rec.Error, at)
	if err != nil {
		return fmt.Errorf("record notification_delivery: %w", err)
	}
	return nil
}
