package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
)

// AuditRepo mengimplementasi audit.Store di atas Postgres. Audit log append-only di
// gov.audit_logs (DB tenant), dirantai hash per tenant untuk deteksi tamper (PR-1.3.2).
type AuditRepo struct {
	pool *Pool
}

func NewAuditRepo(pool *Pool) *AuditRepo { return &AuditRepo{pool: pool} }

var _ audit.Store = (*AuditRepo)(nil)

const auditDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.audit_logs (
    seq           BIGSERIAL,
    id            UUID PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    entity        TEXT NOT NULL,
    entity_id     UUID NOT NULL,
    action        TEXT NOT NULL,
    actor_id      UUID NOT NULL,
    actor_ip      TEXT NOT NULL DEFAULT '',
    diff          JSONB NOT NULL,
    workflow_from TEXT NOT NULL DEFAULT '',
    workflow_to   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL,
    prev_hash     TEXT NOT NULL,
    hash          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON gov.audit_logs (entity, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON gov.audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_seq ON gov.audit_logs (tenant_id, seq);`

// EnsureSchema membuat schema gov & tabel audit bila belum ada. Dipanggil saat boot.
func (r *AuditRepo) EnsureSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, auditDDL)
	return err
}

// Append menyisipkan satu entry, merantainya ke entry terakhir milik tenant yang sama.
// Penulisan diserialisasi per tenant lewat advisory lock transaksi agar chain tidak
// putus oleh penulisan paralel (PRD F3). Append-only: hanya INSERT.
func (r *AuditRepo) Append(ctx context.Context, e audit.AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op bila sudah commit

	// Serialisasi per tenant: pemegang lock berikutnya menunggu sampai commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, e.TenantID); err != nil {
		return err
	}

	// Hash entry terakhir tenant ini = prev_hash entry baru; seed bila belum ada.
	prev := audit.SeedHash
	var last string
	err = tx.QueryRow(ctx,
		`SELECT hash FROM gov.audit_logs WHERE tenant_id = $1 ORDER BY seq DESC LIMIT 1`,
		e.TenantID).Scan(&last)
	switch {
	case err == nil:
		prev = last
	case IsNoRows(err):
		// entry pertama: pakai seed
	default:
		return err
	}

	// Normalisasi timestamp ke presisi penyimpanan sebelum hashing agar konsisten
	// saat dibaca ulang untuk verifikasi.
	e.Timestamp = e.Timestamp.UTC().Truncate(time.Microsecond)
	e.PrevHash = prev
	e.Hash = audit.ComputeHash(prev, e)

	diffJSON, err := json.Marshal(e.Diff)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO gov.audit_logs
			(id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
			 diff, workflow_from, workflow_to, created_at, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.ID, e.TenantID, e.Entity, e.EntityID, string(e.Action), e.ActorID, e.ActorIP,
		diffJSON, e.WorkflowFrom, e.WorkflowTo, e.Timestamp, e.PrevHash, e.Hash,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ByEntity mengembalikan riwayat audit satu entity, terurut kronologis (F5).
func (r *AuditRepo) ByEntity(ctx context.Context, entity string, entityID uuid.UUID) ([]audit.AuditEntry, error) {
	return r.queryEntries(ctx,
		`WHERE entity = $1 AND entity_id = $2 ORDER BY seq ASC`, entity, entityID)
}

// ByTenant mengembalikan seluruh entry tenant terurut chain (untuk verifikasi).
func (r *AuditRepo) ByTenant(ctx context.Context, tenantID string) ([]audit.AuditEntry, error) {
	return r.queryEntries(ctx, `WHERE tenant_id = $1 ORDER BY seq ASC`, tenantID)
}

func (r *AuditRepo) queryEntries(ctx context.Context, where string, args ...any) ([]audit.AuditEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
		       diff, workflow_from, workflow_to, created_at, prev_hash, hash
		FROM gov.audit_logs `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []audit.AuditEntry
	for rows.Next() {
		var e audit.AuditEntry
		var action string
		var diffJSON []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Entity, &e.EntityID, &action,
			&e.ActorID, &e.ActorIP, &diffJSON, &e.WorkflowFrom, &e.WorkflowTo,
			&e.Timestamp, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.Action = audit.Action(action)
		if err := json.Unmarshal(diffJSON, &e.Diff); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
