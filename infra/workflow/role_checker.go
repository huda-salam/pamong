package workflow

import (
	"context"

	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
)

// TenantRoleChecker mengimplementasi coreWf.RoleChecker (PR-N2 bagian C) di atas
// gov.tenant_roles pada tenant DB — dipakai TemplateChoiceManager.SetChoice untuk menolak
// RoleBindings yang menunjuk role tak terdaftar.
//
// Isolasi tenant STRUKTURAL (pool sudah terkoneksi ke tenant DB spesifik, konvensi sama dengan
// infra/notification.DBRecipientDirectory): parameter tenantID diterima demi kesesuaian bentuk
// port (mock/in-memory bisa multi-tenant), tapi diabaikan di query nyata.
type TenantRoleChecker struct {
	repo *tenantroledb.TenantRoleRepo
}

// NewTenantRoleChecker merakit checker dari pool tenant DB.
func NewTenantRoleChecker(pool *db.Pool) *TenantRoleChecker {
	return &TenantRoleChecker{repo: tenantroledb.NewTenantRoleRepo(pool)}
}

var _ coreWf.RoleChecker = (*TenantRoleChecker)(nil)

// RoleExists melaporkan apakah roleName terdaftar di gov.tenant_roles. NOT_FOUND dari repo
// diterjemahkan ke (false, nil) — "tak ditemukan" bukan kegagalan infra, hanya jawaban negatif.
func (c *TenantRoleChecker) RoleExists(ctx context.Context, _ string, roleName string) (bool, error) {
	if _, err := c.repo.FindByName(ctx, roleName); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
