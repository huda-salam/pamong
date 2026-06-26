package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/port"
)

// auditedDelegationRepo mencatat pembuatan delegasi ke gov.audit_logs tenant DB (ADR-003 /
// PRD F5 "delegasi tercatat di audit"), tanpa kode audit di use case. Operasi baca diteruskan
// apa adanya. Pola mirror tenantrole.auditedTenantRoleAssignmentRepo.
type auditedDelegationRepo struct {
	inner  domain.DelegationRepository
	engine *audit.Engine
}

// NewAuditedDelegationRepo membungkus DelegationRepository dengan pencatatan audit.
func NewAuditedDelegationRepo(inner domain.DelegationRepository, engine *audit.Engine) domain.DelegationRepository {
	return &auditedDelegationRepo{inner: inner, engine: engine}
}

func (r *auditedDelegationRepo) Save(ctx context.Context, d *domain.Delegation) error {
	if err := r.inner.Save(ctx, d); err != nil {
		return err
	}
	actx, ok := ctx.(port.AuthContext)
	if !ok {
		return core.ErrValidation("ctx", "mutasi delegasi butuh AuthContext (actor tak diketahui)")
	}
	return r.engine.Record(actx, audit.RecordInput{
		Entity:   "delegation.Delegation",
		EntityID: d.ID,
		Action:   audit.ActionCreate,
		After:    delegationFields(d),
	})
}

func (r *auditedDelegationRepo) ListActiveByDelegatee(ctx context.Context, toUserID uuid.UUID, now time.Time) ([]*domain.Delegation, error) {
	return r.inner.ListActiveByDelegatee(ctx, toUserID, now)
}

func delegationFields(d *domain.Delegation) map[string]any {
	return map[string]any{
		"from_user_id":    d.FromUserID,
		"to_user_id":      d.ToUserID,
		"permissions":     d.Permissions,
		"unit_kerja_id":   d.UnitKerjaID,
		"include_subtree": d.IncludeSubtree,
		"reason":          d.Reason,
		"valid_from":      d.ValidFrom,
		"valid_until":     d.ValidUntil,
	}
}
