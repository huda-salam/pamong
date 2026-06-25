package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// NewRepository adalah factory repository yang menerapkan kebijakan audit EntityDef
// secara otomatis (PR-1.3.3 auto-attach): entity Audited dibungkus pencatat audit,
// entity NotAudited memakai repo polos tanpa overhead. Modul TIDAK menulis kode audit —
// cukup mendeklarasikan Audited{} di EntityDef dan memakai repo dari factory ini.
func NewRepository[T any](pool *Pool, m Mapper[T], def domain.EntityDef, engine *audit.Engine) (port.BaseRepository[T], error) {
	base, err := NewSQLRepository[T](pool, m)
	if err != nil {
		return nil, err
	}
	if !def.IsAudited() {
		return base, nil
	}
	if engine == nil {
		return nil, fmt.Errorf("entity %q Audited tapi audit engine nil", def.Name)
	}
	return &auditedRepo[T]{
		inner:  base,
		mapper: m,
		engine: engine,
		entity: def.Schema + "." + def.Name,
	}, nil
}

// auditedRepo membungkus SQLRepository dan mencatat audit pada setiap mutasi.
// Operasi baca (FindByID, List) diteruskan apa adanya tanpa audit.
type auditedRepo[T any] struct {
	inner  *SQLRepository[T]
	mapper Mapper[T]
	engine *audit.Engine
	entity string // "schema.Entity"
}

var _ port.BaseRepository[struct{}] = (*auditedRepo[struct{}])(nil)

func (r *auditedRepo[T]) FindByID(ctx context.Context, id uuid.UUID) (*T, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *auditedRepo[T]) List(ctx context.Context, f port.ListFilter) (*port.ListResult[T], error) {
	return r.inner.List(ctx, f)
}

func (r *auditedRepo[T]) Save(ctx context.Context, entity *T) error {
	if err := r.inner.Save(ctx, entity); err != nil {
		return err
	}
	return r.record(ctx, audit.ActionCreate, r.mapper.ID(entity), nil, r.fields(entity))
}

func (r *auditedRepo[T]) Update(ctx context.Context, entity *T) error {
	before, err := r.inner.FindByID(ctx, r.mapper.ID(entity))
	if err != nil {
		return err
	}
	if err := r.inner.Update(ctx, entity); err != nil {
		return err
	}
	return r.record(ctx, audit.ActionUpdate, r.mapper.ID(entity), r.fields(before), r.fields(entity))
}

func (r *auditedRepo[T]) SoftDelete(ctx context.Context, id uuid.UUID) error {
	before, err := r.inner.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.inner.SoftDelete(ctx, id); err != nil {
		return err
	}
	return r.record(ctx, audit.ActionDelete, id, r.fields(before), nil)
}

// fields membangun snapshot map dari kolom bisnis entity (lewat Mapper).
func (r *auditedRepo[T]) fields(e *T) map[string]any {
	cols := r.mapper.DataColumns()
	vals := r.mapper.DataValues(e)
	out := make(map[string]any, len(cols))
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}

// record menyusun konteks audit. Mutasi entity Auditable wajib punya AuthContext
// (actor + tenant) — caller use case selalu meneruskannya.
func (r *auditedRepo[T]) record(ctx context.Context, action audit.Action, id uuid.UUID, before, after map[string]any) error {
	actx, ok := ctx.(port.AuthContext)
	if !ok {
		return core.ErrValidation("ctx", "mutasi entity Auditable butuh AuthContext (actor tak diketahui)")
	}
	return r.engine.Record(actx, audit.RecordInput{
		Entity:   r.entity,
		EntityID: id,
		Action:   action,
		Before:   before,
		After:    after,
	})
}
