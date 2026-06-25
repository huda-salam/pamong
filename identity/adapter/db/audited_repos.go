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
