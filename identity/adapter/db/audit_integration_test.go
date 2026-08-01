//go:build integration

package db_test

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

func TestIdentityAudit_AutoRecordedAndChained(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditStore)

	// Repo identity dibungkus dekorator audit (use case tak menulis kode audit).
	persons := mustAuditedPersonRepo(t, mustPersonRepo(t, pool, cr), engine, cr)
	employments := mustAuditedEmploymentRepo(t, mustEmploymentRepo(t, pool, cr), engine, cr)

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
	// Diff create TETAP mencatat NIK sebagai bukti (ADR-002) — tapi tersegel, bukan mentah
	// (REVIEW_BACKLOG E2). Dua sisi yang harus sama-sama benar: tak terbaca tanpa kunci,
	// dan tetap dapat dipulihkan DENGAN kunci. Salah satunya saja = teater keamanan atau
	// bukti yang hilang.
	var nikDiff string
	for _, d := range pEntries[0].Diff {
		if d.Field == "nik" {
			s, ok := d.After.(string)
			if !ok {
				t.Fatalf("nilai nik di diff harus string tersegel, dapat %T: %+v", d.After, d.After)
			}
			nikDiff = s
		}
	}
	if nikDiff == "" {
		t.Fatalf("create person harus mencatat NIK di diff: %+v", pEntries[0].Diff)
	}
	if nikDiff == "3578010101900001" {
		t.Fatal("NIK tersimpan MENTAH di id.audit_logs.diff — E2 belum tertutup")
	}
	ct, err := base64.StdEncoding.DecodeString(nikDiff)
	if err != nil {
		t.Fatalf("nilai diff harus ciphertext base64, dapat %q: %v", nikDiff, err)
	}
	plain, err := cr.Decrypt(ctx, port.RowRef{TenantID: crypto.RealmCentral, RecordID: p.ID.String()}, ct)
	if err != nil {
		t.Fatalf("diff tersegel harus bisa dibuka dengan kunci realm sentral: %v", err)
	}
	if string(plain) != "3578010101900001" {
		t.Fatalf("NIK hasil buka salah: %q", plain)
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
