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
