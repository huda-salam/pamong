package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// Dekorator audit untuk repo role tenant (ADR-003 / auto-attach), tanpa kode audit di use
// case. Berbeda dari role sentral yang ter-audit ke id.audit_logs, mutasi role tenant
// ter-audit ke gov.audit_logs pada TENANT DB (audit.Engine dibangun di atas infra/db.AuditRepo
// schema gov). Operasi baca diteruskan apa adanya.

// --- Tenant role ---

type auditedTenantRoleRepo struct {
	inner  domain.TenantRoleRepository
	engine *audit.Engine
}

// NewAuditedTenantRoleRepo membungkus TenantRoleRepository dengan pencatatan audit.
func NewAuditedTenantRoleRepo(inner domain.TenantRoleRepository, engine *audit.Engine) domain.TenantRoleRepository {
	return &auditedTenantRoleRepo{inner: inner, engine: engine}
}

func (r *auditedTenantRoleRepo) Save(ctx context.Context, role *domain.TenantRole) error {
	if err := r.inner.Save(ctx, role); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "tenantrole.TenantRole", role.ID, audit.ActionCreate, nil, tenantRoleFields(role))
}

func (r *auditedTenantRoleRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.TenantRole, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *auditedTenantRoleRepo) FindByName(ctx context.Context, name string) (*domain.TenantRole, error) {
	return r.inner.FindByName(ctx, name)
}

func (r *auditedTenantRoleRepo) List(ctx context.Context) ([]*domain.TenantRole, error) {
	return r.inner.List(ctx)
}

func tenantRoleFields(r *domain.TenantRole) map[string]any {
	return map[string]any{
		"name":        r.Name,
		"label":       r.Label,
		"description": r.Description,
		"permissions": r.Permissions,
	}
}

// --- Tenant role assignment ---

type auditedTenantRoleAssignmentRepo struct {
	inner  domain.TenantRoleAssignmentRepository
	engine *audit.Engine
}

// NewAuditedTenantRoleAssignmentRepo membungkus TenantRoleAssignmentRepository dengan
// pencatatan audit. Menugaskan role tenant = pemberian wewenang, wajib ter-audit (ADR-003).
func NewAuditedTenantRoleAssignmentRepo(inner domain.TenantRoleAssignmentRepository, engine *audit.Engine) domain.TenantRoleAssignmentRepository {
	return &auditedTenantRoleAssignmentRepo{inner: inner, engine: engine}
}

func (r *auditedTenantRoleAssignmentRepo) Save(ctx context.Context, a *domain.TenantRoleAssignment) error {
	if err := r.inner.Save(ctx, a); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "tenantrole.TenantRoleAssignment", a.ID, audit.ActionCreate, nil, tenantAssignmentFields(a))
}

func (r *auditedTenantRoleAssignmentRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*domain.TenantRoleAssignment, error) {
	return r.inner.ListByUser(ctx, userID)
}

func tenantAssignmentFields(a *domain.TenantRoleAssignment) map[string]any {
	return map[string]any{
		"user_id":       a.UserID,
		"role_id":       a.RoleID,
		"unit_kerja_id": a.UnitKerjaID,
		"assigned_by":   a.AssignedBy,
		"valid_from":    a.ValidFrom,
		"valid_until":   a.ValidUntil,
	}
}

// recordAudit menyusun konteks audit. Mutasi role tenant wajib punya AuthContext (actor) —
// use case selalu meneruskannya.
func recordAudit(ctx context.Context, engine *audit.Engine, entity string, id uuid.UUID, action audit.Action, before, after map[string]any) error {
	actx, ok := ctx.(port.AuthContext)
	if !ok {
		return core.ErrValidation("ctx", "mutasi role tenant butuh AuthContext (actor tak diketahui)")
	}
	return engine.Record(actx, audit.RecordInput{
		Entity:   entity,
		EntityID: id,
		Action:   action,
		Before:   before,
		After:    after,
	})
}
