package main

import (
	"context"
	"fmt"

	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/spf13/cobra"
)

// auditCmd: operasi audit trail (PR-1.3.2: verify hash chain).
func auditCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "audit",
		Short: "Operasi jejak audit (verifikasi integritas hash chain)",
	}
	c.AddCommand(auditVerifyCmd())
	return c
}

func auditVerifyCmd() *cobra.Command {
	var tenant string
	c := &cobra.Command{
		Use:   "verify",
		Short: "Telusuri hash chain audit & deteksi manipulasi",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuditVerify(cmd, tenant)
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "tenant_id yang diverifikasi (default: GOV_TENANT_ID)")
	return c
}

func runAuditVerify(cmd *cobra.Command, tenant string) error {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("muat config: %w", err)
	}
	if tenant == "" {
		tenant = cfg.TenantID
	}
	if tenant == "" {
		return fmt.Errorf("tenant tidak ditentukan: pakai --tenant atau set GOV_TENANT_ID")
	}

	pool, err := db.New(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("koneksi DB: %w", err)
	}
	defer pool.Close()

	repo := db.NewAuditRepo(pool)
	entries, err := repo.ByTenant(ctx, tenant)
	if err != nil {
		return fmt.Errorf("ambil audit log: %w", err)
	}

	res := audit.VerifyChain(entries)
	out := cmd.OutOrStdout()
	if res.OK {
		fmt.Fprintf(out, "OK — %d entry, hash chain utuh untuk tenant %q\n", len(entries), tenant)
		return nil
	}
	fmt.Fprintf(out, "TAMPER TERDETEKSI pada entry #%d (id=%s): %s\n", res.BrokenAt, res.EntryID, res.Reason)
	return fmt.Errorf("verifikasi gagal: chain putus")
}
