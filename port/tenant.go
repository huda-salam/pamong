package port

import "context"

// TenantInfo adalah data routing minimal satu tenant yang dibaca gateway dari registry
// sentral (id.tenant_registry). Cukup untuk menentukan lokasi DB tenant + status aktif.
type TenantInfo struct {
	TenantID string
	Tier     int
	DBHost   string
	DBName   string
	DBSchema string
	IsActive bool
}

// TenantResolver me-resolve tenant_id ke info routing-nya. Diimplementasi oleh identity
// (membaca id.tenant_registry); dipakai gateway lewat port ini tanpa import identity/.
type TenantResolver interface {
	Resolve(ctx context.Context, tenantID string) (*TenantInfo, error)
}

// tenantKey adalah kunci privat untuk menyimpan tenant_id di context.Context.
type tenantKey struct{}

// WithTenant menyisipkan tenant_id ke context. Dipakai driving adapter (middleware tenant
// resolver) agar driven adapter — khususnya router DB per-tenant (DB-per-tenant, ADR-004) —
// bisa memilih koneksi tenant yang benar TANPA bergantung pada tipe context konkret.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFrom mengembalikan tenant_id yang berlaku untuk context ini, atau "" bila tak ada.
//
// Dua sumber, berurutan: (1) nilai eksplisit yang disisipkan WithTenant; (2) fallback bila
// context itu sendiri sebuah AuthContext (mis. gateway.Context yang dibawa handler ke use
// case), pakai TenantID()-nya.
//
// DEFERRED(Phase-5.1.2): fallback (2) RAPUH — assertion ctx.(AuthContext) hanya cocok bila ctx
// adalah gateway.Context telanjang. Begitu ada lapisan yang membungkus (context.WithValue/
// WithTimeout/WithCancel) di rantai handler→usecase→repo, ctx bukan lagi AuthContext dan
// fallback mengembalikan "" → routing gagal. Jalur yang benar & tahan-bungkus adalah WithTenant
// eksplisit yang disisipkan middleware tenant resolver (PR-5.1.2); fallback ini hanya jembatan
// sementara dan JANGAN diandalkan untuk jalur yang membungkus context.
func TenantFrom(ctx context.Context) string {
	if v, ok := ctx.Value(tenantKey{}).(string); ok && v != "" {
		return v
	}
	if ac, ok := ctx.(AuthContext); ok {
		return ac.TenantID()
	}
	return ""
}
