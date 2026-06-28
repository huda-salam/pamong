// Package eventbus — outbox.go: implementasi outbox pattern (PRD eventbus F3).
// Event ditulis ke gov.outbox_events dalam transaksi bisnis yang sama; relay
// membacanya setelah commit dan mengirim via bus driver.
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

const outboxDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.outbox_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    tenant_id       TEXT NOT NULL DEFAULT '',
    caused_by       TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    dispatched_at   TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0,
    next_retry_at   TIMESTAMPTZ,
    failed_at       TIMESTAMPTZ
);
-- Idempotent migration: tambah kolom baru ke tabel yang sudah ada.
ALTER TABLE gov.outbox_events ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;
ALTER TABLE gov.outbox_events ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ;
-- Index mencakup filter DLQ + backoff sehingga hanya baris siap-kirim yang di-scan.
DROP INDEX IF EXISTS idx_outbox_pending;
CREATE INDEX IF NOT EXISTS idx_outbox_pending
    ON gov.outbox_events (next_retry_at NULLS FIRST, created_at)
    WHERE dispatched_at IS NULL AND failed_at IS NULL;`

// EnsureOutboxSchema membuat schema gov dan tabel gov.outbox_events bila belum ada.
// Dipanggil saat bootstrap sebelum relay dijalankan.
func EnsureOutboxSchema(ctx context.Context, conn port.DBConn) error {
	_, err := conn.Exec(ctx, outboxDDL)
	return err
}

// OutboxStore implementasi port.EventPublisher yang menulis ke gov.outbox_events
// dalam koneksi yang diberikan. conn wajib berupa *db.Tx (transaksi bisnis yang
// sedang berjalan) agar INSERT outbox bersifat atomik dengan mutasi data bisnis —
// rollback transaksi membatalkan event sekaligus (PRD eventbus F3).
type OutboxStore struct {
	conn   port.DBConn
	schema *SchemaRegistry
}

var _ port.EventPublisher = (*OutboxStore)(nil)

// NewOutboxStore membuat OutboxStore. conn biasanya *db.Tx dari transaksi use case.
func NewOutboxStore(conn port.DBConn, schema *SchemaRegistry) *OutboxStore {
	return &OutboxStore{conn: conn, schema: schema}
}

// Publish memvalidasi event terhadap schema registry lalu INSERT ke outbox.
// Dipanggil oleh use case dalam transaksi yang sama dengan mutasi bisnis.
func (s *OutboxStore) Publish(ctx context.Context, event port.Event) error {
	if err := s.schema.Validate(event.Name, event.Payload); err != nil {
		return err
	}
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload event %q: %w", event.Name, err)
	}
	_, err = s.conn.Exec(ctx, `
		INSERT INTO gov.outbox_events
			(id, event_name, payload, tenant_id, caused_by, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), event.Name, payloadBytes,
		event.TenantID, event.CausedBy, event.IdempotencyKey,
	)
	return err
}

// OutboxRelay membaca event pending dari gov.outbox_events dan mengirimkannya via
// bus driver. Berjalan sebagai goroutine background yang di-Start saat bootstrap.
type OutboxRelay struct {
	pool      *db.Pool
	bus       *Bus
	interval  time.Duration
	batchSize int
	policy    RetryPolicy
}

// NewOutboxRelay membuat relay dengan interval polling dan RetryPolicy default.
// batchSize default 10.
func NewOutboxRelay(pool *db.Pool, bus *Bus, interval time.Duration) *OutboxRelay {
	return &OutboxRelay{
		pool:      pool,
		bus:       bus,
		interval:  interval,
		batchSize: 10,
		policy:    DefaultRetryPolicy(),
	}
}

// WithRetryPolicy mengganti kebijakan retry default. Kembalikan relay itu sendiri
// agar bisa di-chain: NewOutboxRelay(...).WithRetryPolicy(p).
func (r *OutboxRelay) WithRetryPolicy(p RetryPolicy) *OutboxRelay {
	r.policy = p
	return r
}

// Start menjalankan goroutine polling hingga ctx dibatalkan. Non-blocking.
func (r *OutboxRelay) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = r.RunOnce(ctx)
			}
		}
	}()
}

type outboxRow struct {
	id             uuid.UUID
	eventName      string
	payload        []byte
	tenantID       string
	causedBy       string
	idempotencyKey string
	attempts       int
}

// RunOnce menjalankan satu siklus poll: kunci baris pending, dispatch, tandai selesai.
// Di-expose agar test bisa memanggil langsung tanpa ticker.
//
// Jaminan at-least-once: bila crash setelah Dispatch tapi sebelum UPDATE dispatched_at,
// event akan dikirim ulang pada poll berikutnya. Consumer wajib idempoten (PRD F5).
//
// Backoff & DLQ: bila Dispatch gagal, relay menyimpan next_retry_at (backoff eksponensial).
// Setelah attempts >= RetryPolicy.MaxAttempts, baris di-mark failed_at (DLQ) dan tidak
// di-poll lagi sampai operator me-reset via UPDATE SET failed_at=NULL, attempts=0.
func (r *OutboxRelay) RunOnce(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("outbox relay begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// SELECT FOR UPDATE SKIP LOCKED: aman untuk multi-instance relay — tiap instance
	// hanya mengambil baris yang belum dikunci instance lain.
	// Filter: hanya baris yang belum dispatched, belum DLQ, dan sudah lewat next_retry_at.
	rows, err := tx.Query(ctx, `
		SELECT id, event_name, payload, tenant_id, caused_by, idempotency_key, attempts
		FROM gov.outbox_events
		WHERE dispatched_at IS NULL
		  AND failed_at IS NULL
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, r.batchSize)
	if err != nil {
		return fmt.Errorf("outbox relay select: %w", err)
	}

	var batch []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.eventName, &row.payload,
			&row.tenantID, &row.causedBy, &row.idempotencyKey, &row.attempts); err != nil {
			rows.Close()
			return fmt.Errorf("outbox relay scan: %w", err)
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("outbox relay rows: %w", err)
	}

	for _, row := range batch {
		payload, err := r.bus.schema.Unmarshal(row.eventName, row.payload)
		if err != nil {
			// Schema belum terdaftar (mis. race saat startup) — lewati, coba lagi berikutnya.
			continue
		}
		event := port.Event{
			Name:           row.eventName,
			Payload:        payload,
			TenantID:       row.tenantID,
			CausedBy:       row.causedBy,
			IdempotencyKey: row.idempotencyKey,
		}
		if err := r.bus.driver.Dispatch(ctx, event); err != nil {
			newAttempts := row.attempts + 1
			nextRetry, isDLQ := r.policy.NextRetry(newAttempts)
			if isDLQ {
				slog.Error("outbox event masuk DLQ setelah N kali gagal",
					"event", row.eventName,
					"id", row.id,
					"attempts", newAttempts,
					"err", err,
					"dlq", true,
				)
				_, _ = tx.Exec(ctx,
					`UPDATE gov.outbox_events
					 SET attempts = $2, failed_at = now(), next_retry_at = NULL
					 WHERE id = $1`, row.id, newAttempts)
			} else {
				_, _ = tx.Exec(ctx,
					`UPDATE gov.outbox_events
					 SET attempts = $2, next_retry_at = $3
					 WHERE id = $1`, row.id, newAttempts, nextRetry)
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gov.outbox_events SET dispatched_at = now() WHERE id = $1`, row.id); err != nil {
			return fmt.Errorf("outbox relay mark dispatched id=%s: %w", row.id, err)
		}
	}
	return tx.Commit(ctx)
}
