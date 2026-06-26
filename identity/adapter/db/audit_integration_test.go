//go:build integration

package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/testkit"
)

func TestIdentityAudit_AutoRecordedAndChained(t *testing.T) {
	pool, ctx := setupIdentityDB(t)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditStore)

	// Repo identity dibungkus dekorator audit (use case tak menulis kode audit).
	persons := db.NewAuditedPersonRepo(db.NewPersonRepo(pool), engine)
	employments := db.NewAuditedEmploymentRepo(db.NewEmploymentRepo(pool), engine)

	actor := uuid.New()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermPersonBuat),
		testkit.WithPermission(domain.PermEmploymentLampir),
	)

	// Jalankan use case nyata. Publisher memory-less (mock) — audit yang diuji di sini.
	pub := testkit.NewMockPublisher()
	p, err := usecase.NewCreatePerson(persons, pub).Execute(actx, usecase.CreatePersonInput{
		NIK: "3578010101900001", NamaLengkap: "Budi",
	})
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	if _, err := usecase.NewAttachEmployment(persons, employments, pub).Execute(actx, usecase.AttachEmploymentInput{
		PersonID: p.ID, Status: domain.StatusASN, NIP: "199001012015011001",
	}); err != nil {
		t.Fatalf("attach employment: %v", err)
	}

	// Audit person tercatat otomatis dengan actor benar.
	pEntries, err := auditStore.ByEntity(ctx, "identity.Person", p.ID)
	if err != nil {
		t.Fatalf("byEntity person: %v", err)
	}
	if len(pEntries) != 1 || pEntries[0].Action != audit.ActionCreate || pEntries[0].ActorID != actor {
		t.Fatalf("audit person salah: %+v", pEntries)
	}
	// Diff create mencatat NIK.
	var nikRecorded bool
	for _, d := range pEntries[0].Diff {
		if d.Field == "nik" && d.After == "3578010101900001" {
			nikRecorded = true
		}
	}
	if !nikRecorded {
		t.Fatalf("create person harus mencatat NIK di diff: %+v", pEntries[0].Diff)
	}

	// Chain identity (person + employment) utuh.
	chain, err := auditStore.Chain(ctx)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("harus 2 entry audit identity, dapat %d", len(chain))
	}
	if res := audit.VerifyChain(chain); !res.OK {
		t.Fatalf("chain identity harus utuh, dapat %+v", res)
	}

	// Manipulasi langsung → terdeteksi.
	if _, err := pool.Exec(ctx,
		`UPDATE id.audit_logs SET diff = '[{"field":"nik","before":null,"after":"0000000000000000"}]'::jsonb
		 WHERE entity = 'identity.Person'`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	chain, _ = auditStore.Chain(ctx)
	if res := audit.VerifyChain(chain); res.OK {
		t.Fatal("manipulasi audit identity harus terdeteksi")
	}
}
