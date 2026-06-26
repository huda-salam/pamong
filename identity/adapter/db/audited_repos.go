package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Dekorator audit untuk repo identity (ADR-003 / auto-attach). Membungkus port domain
// dan mencatat audit pada mutasi, tanpa kode audit di use case. Operasi baca diteruskan.

// --- Person ---

type auditedPersonRepo struct {
	inner  domain.PersonRepository
	engine *audit.Engine
}

// NewAuditedPersonRepo membungkus PersonRepository dengan pencatatan audit.
func NewAuditedPersonRepo(inner domain.PersonRepository, engine *audit.Engine) domain.PersonRepository {
	return &auditedPersonRepo{inner: inner, engine: engine}
}

func (r *auditedPersonRepo) Save(ctx context.Context, p *domain.Person) error {
	if err := r.inner.Save(ctx, p); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "identity.Person", p.ID, audit.ActionCreate, nil, personFields(p))
}

func (r *auditedPersonRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Person, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *auditedPersonRepo) FindByNIK(ctx context.Context, nik string) (*domain.Person, error) {
	return r.inner.FindByNIK(ctx, nik)
}

func personFields(p *domain.Person) map[string]any {
	return map[string]any{
		"nik":          p.NIK,
		"nama_lengkap": p.NamaLengkap,
		"no_hp":        p.NoHP,
		"email":        p.Email,
		"is_active":    p.IsActive,
		"tgl_lahir":    p.TglLahir,
	}
}

// --- Employment ---

type auditedEmploymentRepo struct {
	inner  domain.EmploymentRepository
	engine *audit.Engine
}

// NewAuditedEmploymentRepo membungkus EmploymentRepository dengan pencatatan audit.
func NewAuditedEmploymentRepo(inner domain.EmploymentRepository, engine *audit.Engine) domain.EmploymentRepository {
	return &auditedEmploymentRepo{inner: inner, engine: engine}
}

func (r *auditedEmploymentRepo) Save(ctx context.Context, e *domain.Employment) error {
	if err := r.inner.Save(ctx, e); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "identity.Employment", e.ID, audit.ActionCreate, nil, employmentFields(e))
}

func (r *auditedEmploymentRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Employment, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *auditedEmploymentRepo) FindByNIP(ctx context.Context, nip string) (*domain.Employment, error) {
	return r.inner.FindByNIP(ctx, nip)
}

func (r *auditedEmploymentRepo) ListByPerson(ctx context.Context, personID uuid.UUID) ([]*domain.Employment, error) {
	return r.inner.ListByPerson(ctx, personID)
}

func employmentFields(e *domain.Employment) map[string]any {
	return map[string]any{
		"person_id":     e.PersonID,
		"status":        string(e.Status),
		"nip":           e.NIP,
		"instansi_asal": e.InstansiAsal,
		"is_active":     e.IsActive,
		"valid_from":    e.ValidFrom,
		"valid_until":   e.ValidUntil,
	}
}

// --- Tenant registry ---

type auditedTenantRepo struct {
	inner  domain.TenantRegistry
	engine *audit.Engine
}

// NewAuditedTenantRepo membungkus TenantRegistry dengan pencatatan audit. Entity audit
// memakai EntityID nil (tenant ber-natural-key string); identitas tenant ada di diff.
func NewAuditedTenantRepo(inner domain.TenantRegistry, engine *audit.Engine) domain.TenantRegistry {
	return &auditedTenantRepo{inner: inner, engine: engine}
}

func (r *auditedTenantRepo) Save(ctx context.Context, t *domain.Tenant) error {
	if err := r.inner.Save(ctx, t); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "identity.Tenant", tenantAuditID(t.TenantID),
		audit.ActionCreate, nil, tenantFields(t))
}

func (r *auditedTenantRepo) SetActive(ctx context.Context, tenantID string, active bool) error {
	before, err := r.inner.FindByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if err := r.inner.SetActive(ctx, tenantID, active); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "identity.Tenant", tenantAuditID(tenantID), audit.ActionUpdate,
		map[string]any{"is_active": before.IsActive}, map[string]any{"is_active": active})
}

func (r *auditedTenantRepo) FindByID(ctx context.Context, tenantID string) (*domain.Tenant, error) {
	return r.inner.FindByID(ctx, tenantID)
}

func (r *auditedTenantRepo) List(ctx context.Context) ([]*domain.Tenant, error) {
	return r.inner.List(ctx)
}

func tenantFields(t *domain.Tenant) map[string]any {
	return map[string]any{
		"tenant_id": t.TenantID, "nama": t.Nama, "tier": t.Tier,
		"db_host": t.DBHost, "db_name": t.DBName, "is_active": t.IsActive,
	}
}

// --- Tenant assignment ---

type auditedTenantAssignmentRepo struct {
	inner  domain.TenantAssignmentRepository
	engine *audit.Engine
}

// NewAuditedTenantAssignmentRepo membungkus TenantAssignmentRepository dengan pencatatan
// audit. Penugasan ke tenant adalah mutasi identitas sensitif — wajib ter-audit (ADR-003).
func NewAuditedTenantAssignmentRepo(inner domain.TenantAssignmentRepository, engine *audit.Engine) domain.TenantAssignmentRepository {
	return &auditedTenantAssignmentRepo{inner: inner, engine: engine}
}

func (r *auditedTenantAssignmentRepo) Save(ctx context.Context, a *domain.TenantAssignment) error {
	if err := r.inner.Save(ctx, a); err != nil {
		return err
	}
	return recordAudit(ctx, r.engine, "identity.TenantAssignment", a.ID, audit.ActionCreate, nil, tenantAssignmentFields(a))
}

func (r *auditedTenantAssignmentRepo) ListByEmployment(ctx context.Context, employmentID uuid.UUID) ([]*domain.TenantAssignment, error) {
	return r.inner.ListByEmployment(ctx, employmentID)
}

func tenantAssignmentFields(a *domain.TenantAssignment) map[string]any {
	return map[string]any{
		"employment_id":  a.EmploymentID,
		"tenant_id":      a.TenantID,
		"is_home_tenant": a.IsHomeTenant,
		"assigned_by":    a.AssignedBy,
		"valid_from":     a.ValidFrom,
		"valid_until":    a.ValidUntil,
	}
}

// tenantAuditID menurunkan UUID deterministik dari tenant_id (natural key string) agar
// muat di kolom entity_id audit dan riwayat per-tenant bisa di-query konsisten.
func tenantAuditID(tenantID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("pamong:tenant:"+tenantID))
}

// recordAudit menyusun konteks audit. Mutasi identity wajib punya AuthContext (actor) —
// use case selalu meneruskannya.
func recordAudit(ctx context.Context, engine *audit.Engine, entity string, id uuid.UUID, action audit.Action, before, after map[string]any) error {
	actx, ok := ctx.(port.AuthContext)
	if !ok {
		return core.ErrValidation("ctx", "mutasi identity butuh AuthContext (actor tak diketahui)")
	}
	return engine.Record(actx, audit.RecordInput{
		Entity:   entity,
		EntityID: id,
		Action:   action,
		Before:   before,
		After:    after,
	})
}
