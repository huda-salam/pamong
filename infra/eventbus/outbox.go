// Package eventbus — outbox.go: implementasi outbox pattern (PRD eventbus F3).
// Event ditulis ke gov.outbox_events dalam transaksi bisnis yang sama; relay
// membacanya setelah commit dan mengirim via bus driver.
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
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
    attempts        INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending
    ON gov.outbox_events (created_at)
    WHERE dispatched_at IS NULL;`

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
}

// NewOutboxRelay membuat relay dengan interval polling. batchSize default 10.
func NewOutboxRelay(pool *db.Pool, bus *Bus, interval time.Duration) *OutboxRelay {
	return &OutboxRelay{pool: pool, bus: bus, interval: interval, batchSize: 10}
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
}

// RunOnce menjalankan satu siklus poll: kunci baris pending, dispatch, tandai selesai.
// Di-expose agar test bisa memanggil langsung tanpa ticker.
//
// Jaminan at-least-once: bila crash setelah Dispatch tapi sebelum UPDATE dispatched_at,
// event akan dikirim ulang pada poll berikutnya. Consumer wajib idempoten (PRD F5).
func (r *OutboxRelay) RunOnce(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("outbox relay begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// SELECT FOR UPDATE SKIP LOCKED: aman untuk multi-instance relay — tiap instance
	// hanya mengambil baris yang belum dikunci instance lain.
	rows, err := tx.Query(ctx, `
		SELECT id, event_name, payload, tenant_id, caused_by, idempotency_key
		FROM gov.outbox_events
		WHERE dispatched_at IS NULL
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
			&row.tenantID, &row.causedBy, &row.idempotencyKey); err != nil {
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
		// Dispatch langsung ke driver — schema sudah divalidasi saat INSERT ke outbox.
		if err := r.bus.driver.Dispatch(ctx, event); err != nil {
			// DEFERRED(PR-3.1.4): DLQ setelah N kali gagal.
			_, _ = tx.Exec(ctx,
				`UPDATE gov.outbox_events SET attempts = attempts + 1 WHERE id = $1`, row.id)
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gov.outbox_events SET dispatched_at = now() WHERE id = $1`, row.id); err != nil {
			return fmt.Errorf("outbox relay mark dispatched id=%s: %w", row.id, err)
		}
	}
	return tx.Commit(ctx)
}
