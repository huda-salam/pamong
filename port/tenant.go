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
// Satu-satunya sumber: nilai eksplisit yang disisipkan WithTenant. Middleware tenant resolver
// (PR-5.1.2) menyuntikkan tenant ke embedded context gateway.Context lewat SetTenantID →
// port.WithTenant, sehingga nilai ini bertahan menembus pembungkusan context (WithValue/
// WithTimeout/WithCancel) di rantai handler→usecase→repo.
//
// Catatan: fallback lama via assertion ctx.(AuthContext) DIHAPUS (menutup DEFERRED
// Phase-5.1.2 #2) — ia pecah begitu context dibungkus. Jalur ber-tenant kini WAJIB lewat
// WithTenant eksplisit; context tanpa WithTenant → "" (routing gagal eksplisit, bukan diam).
func TenantFrom(ctx context.Context) string {
	if v, ok := ctx.Value(tenantKey{}).(string); ok {
		return v
	}
	return ""
}
