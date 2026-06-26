package db

import (
	"context"

	"github.com/huda-salam/pamong/infra/db"
)

// delegationDDL membuat schema gov + gov.delegations bila belum ada. EnsureSchema-on-write
// (preseden gov.user_profiles / gov.tenant_roles): tabel framework gov.* belum punya runner
// migrasi formal — DEFERRED ke runner migrasi framework-gov (lihat ROADMAP). Idempoten via
// IF NOT EXISTS. valid_until NOT NULL menegakkan invariant "delegasi selalu berbatas waktu".
const delegationDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.delegations (
    id              UUID PRIMARY KEY,
    from_user_id    UUID NOT NULL,
    to_user_id      UUID NOT NULL,
    permissions     TEXT[] NOT NULL,
    unit_kerja_id   UUID,
    include_subtree BOOLEAN NOT NULL DEFAULT false,
    reason          TEXT,
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until     TIMESTAMPTZ NOT NULL,
    assigned_by     UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_delegations_to_user ON gov.delegations (to_user_id);`

func ensureDelegationSchema(ctx context.Context, exec db.Conn) error {
	_, err := exec.Exec(ctx, delegationDDL)
	return err
}
