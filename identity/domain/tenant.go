package domain

import (
	"regexp"
	"time"
)

// Tenant registry (PR-2.2.1). Registry routing tenant hidup di identity DB sentral
// (id.tenant_registry), BUKAN tenant DB: resolver harus tahu lokasi DB tenant sebelum
// bisa connect ke sana (chicken-and-egg) — karena itu wajib sentral. Menyimpan tier
// portabilitas + koneksi DB + status aktif.

var tenantIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{2,99}$`)

// Tier portabilitas tenant (CLAUDE.md "Tenant tier & portabilitas").
const (
	TierShared          = 1 // shared Postgres, DB per-tenant
	TierDedicatedDB     = 2 // dedicated DB server
	TierDedicatedServer = 3 // server/VPS milik pemda
)

// Tenant adalah satu entri registry: identitas tenant + lokasi DB + status.
type Tenant struct {
	TenantID         string // natural key, mis. "pemkot-surabaya"
	Nama             string
	Tier             int
	DBHost           string
	DBName           string
	DBSchema         string // opsional; kosong = pakai schema per-modul default
	MigrationVersion string
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate memeriksa invariant tenant tanpa I/O.
func (t *Tenant) Validate() error {
	if !tenantIDRe.MatchString(t.TenantID) {
		return ErrTenantIDInvalid
	}
	if t.Nama == "" {
		return ErrTenantNamaKosong
	}
	if t.Tier < TierShared || t.Tier > TierDedicatedServer {
		return ErrTenantTierInvalid
	}
	if t.DBHost == "" || t.DBName == "" {
		return ErrTenantDBKosong
	}
	return nil
}
