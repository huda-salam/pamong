package customization

import (
	"context"

	"github.com/huda-salam/pamong/core/config"
)

// Label override (PRD F2) sengaja TIDAK memakai tabel/store sendiri: ia menumpang pada tenant
// config ber-scope yang sudah ada (core/config) dengan konvensi key di bawah. Alasannya, label
// override adalah "nilai per-tenant ber-scope" — persis yang di-handle Resolver (titik ekstensi
// #2) — sehingga hemat satu tabel, satu resolver, dan satu adapter Postgres yang sudah teruji
// (infra/config). Scope-nya level tenant (ConfigScope{TenantID}); bila kelak butuh per-unit,
// resolver yang sama sudah mendukung tanpa migrasi.
//
// Catatan: framework belum memiliki slot Label pada domain.FieldDef (menyusul bersama metadata
// UI field, ERP reference study). Karena itu label override kini TERSIMPAN & TER-RESOLVE sebagai
// data; penerapannya ke form dilakukan lapis UI saat FieldUI hadir. DEFERRED(Phase-3.8/UI): merge
// label ke definisi field yang dirender.

const labelKeyPrefix = "customization.label."

// LabelKey membentuk key config untuk override label satu field: unik per (module, entity, field).
func LabelKey(module, entity, field string) string {
	return labelKeyPrefix + module + "." + entity + "." + field
}

// LabelResolver membaca label override per-tenant di atas config.Resolver. ok=false berarti
// tenant tak meng-override — pemakai memakai label bawaan field.
type LabelResolver struct {
	resolver *config.Resolver
}

// NewLabelResolver merakit resolver label di atas resolver config.
func NewLabelResolver(r *config.Resolver) *LabelResolver {
	return &LabelResolver{resolver: r}
}

// Label mengembalikan label override yang berlaku untuk (tenant, module, entity, field).
func (l *LabelResolver) Label(ctx context.Context, tenantID, module, entity, field string) (string, bool, error) {
	return l.resolver.Resolve(ctx, config.ConfigScope{TenantID: tenantID}, LabelKey(module, entity, field))
}
