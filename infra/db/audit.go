package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/port"
)

// AuditRepo mengimplementasi audit.Store di atas port.DBConn. Audit log append-only
// disimpan di gov.audit_logs (DB tenant). Diff disimpan sebagai JSONB.
type AuditRepo struct {
	conn port.DBConn
}

func NewAuditRepo(conn port.DBConn) *AuditRepo { return &AuditRepo{conn: conn} }

var _ audit.Store = (*AuditRepo)(nil)

const auditDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.audit_logs (
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
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON gov.audit_logs (entity, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON gov.audit_logs (actor_id);`

// EnsureSchema membuat schema gov & tabel audit bila belum ada. Dipanggil saat boot.
func (r *AuditRepo) EnsureSchema(ctx context.Context) error {
	_, err := r.conn.Exec(ctx, auditDDL)
	return err
}

// Append menyisipkan satu entry audit. Append-only: hanya INSERT.
func (r *AuditRepo) Append(ctx context.Context, e audit.AuditEntry) error {
	diffJSON, err := json.Marshal(e.Diff)
	if err != nil {
		return err
	}
	_, err = r.conn.Exec(ctx, `
		INSERT INTO gov.audit_logs
			(id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
			 diff, workflow_from, workflow_to, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, e.TenantID, e.Entity, e.EntityID, string(e.Action), e.ActorID, e.ActorIP,
		diffJSON, e.WorkflowFrom, e.WorkflowTo, e.Timestamp,
	)
	return err
}

// ByEntity mengembalikan riwayat audit satu entity, terurut kronologis (F5).
func (r *AuditRepo) ByEntity(ctx context.Context, entity string, entityID uuid.UUID) ([]audit.AuditEntry, error) {
	rows, err := r.conn.Query(ctx, `
		SELECT id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
		       diff, workflow_from, workflow_to, created_at
		FROM gov.audit_logs
		WHERE entity = $1 AND entity_id = $2
		ORDER BY created_at ASC`, entity, entityID)
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
			&e.ActorID, &e.ActorIP, &diffJSON, &e.WorkflowFrom, &e.WorkflowTo, &e.Timestamp); err != nil {
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
