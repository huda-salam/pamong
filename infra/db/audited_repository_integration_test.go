//go:build integration

package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/testkit"
)

func TestAutoAttach_AuditedEntity_RecordsMutations(t *testing.T) {
	repo, ctx := newTestRepo(t) // menyiapkan schema test_repo.produk + pool
	_ = repo

	pool, _ := newTestPool(t) // pool kedua ke DB yang sama untuk audit
	auditRepo := db.NewAuditRepo(pool)
	if err := auditRepo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit: %v", err)
	}
	engine := audit.NewEngine(auditRepo)

	def := domain.EntityDef{
		Name: "Produk", Schema: "test_repo", Tier: domain.Tier1,
		Audit: domain.Audited{}, Lockable: domain.NotLockable{},
	}
	aRepo, err := db.NewRepository[produk](pool, produkMapper{}, def, engine)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	actor := uuid.New()
	actx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"), testkit.WithPersonID(actor))

	p := &produk{ID: uuid.New(), Nama: "Meja", Harga: 500000}
	if err := aRepo.Save(actx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	p.Harga = 550000
	if err := aRepo.Update(actx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := aRepo.SoftDelete(actx, p.ID); err != nil {
		t.Fatalf("softDelete: %v", err)
	}

	entries, err := auditRepo.ByEntity(ctx, "test_repo.Produk", p.ID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("harus 3 entry audit (create/update/delete), dapat %d", len(entries))
	}
	if entries[0].Action != audit.ActionCreate ||
		entries[1].Action != audit.ActionUpdate ||
		entries[2].Action != audit.ActionDelete {
		t.Fatalf("urutan aksi salah: %s/%s/%s", entries[0].Action, entries[1].Action, entries[2].Action)
	}
	// Update mencatat perubahan harga 500000 -> 550000.
	var hargaChanged bool
	for _, d := range entries[1].Diff {
		if d.Field == "harga" {
			hargaChanged = true
		}
	}
	if !hargaChanged {
		t.Fatalf("update harus mencatat diff harga, dapat %+v", entries[1].Diff)
	}
	if entries[0].ActorID != actor || entries[0].TenantID != "pemkot-surabaya" {
		t.Fatalf("actor/tenant tidak terekam: %+v", entries[0])
	}
}

func TestAutoAttach_NotAuditedEntity_NoAudit(t *testing.T) {
	_, ctx := newTestRepo(t)
	pool, _ := newTestPool(t)
	auditRepo := db.NewAuditRepo(pool)
	if err := auditRepo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit: %v", err)
	}

	def := domain.EntityDef{
		Name: "Produk", Schema: "test_repo", Tier: domain.Tier1,
		Audit: domain.NotAudited{Reason: "data sementara"}, Lockable: domain.NotLockable{},
	}
	repo, err := db.NewRepository[produk](pool, produkMapper{}, def, audit.NewEngine(auditRepo))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	p := &produk{ID: uuid.New(), Nama: "Kursi", Harga: 100}
	actx := testkit.Ctx(t, testkit.WithTenant("t"), testkit.WithPersonID(uuid.New()))
	if err := repo.Save(actx, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	entries, err := auditRepo.ByEntity(ctx, "test_repo.Produk", p.ID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("NotAudited tidak boleh menghasilkan audit, dapat %d", len(entries))
	}
}
