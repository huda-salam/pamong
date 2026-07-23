package workflow_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// ===== Compile: syntax & type errors ketahuan saat load =====

func TestCompile_SyntaxError(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"kurung tak seimbang", "(actor.is_citizen"},
		{"operator gantung", "actor.is_citizen &&"},
		{"amp tunggal", "actor.is_citizen & actor.is_cross_tenant"},
		{"pipe tunggal", "actor.is_citizen | actor.is_cross_tenant"},
		{"sama-dengan tunggal", "actor.persona = 'employee'"},
		{"string tak ditutup", "actor.persona == 'employee"},
		{"root tak dikenal", "user.is_admin"},
		{"actor tanpa member", "actor"},
		{"fungsi actor tak dikenal", "actor.has_superpower('x')"},
		{"properti actor tak dikenal", "actor.is_admin"},
		{"has_permission tanpa arg string", "actor.has_permission(123)"},
		{"karakter asing", "actor.is_citizen ~ true"},
		{"token sisa", "true false"},
		{"ekspresi kosong", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := workflow.Compile(c.expr); err == nil {
				t.Fatalf("Compile(%q) diharapkan error, dapat nil", c.expr)
			}
		})
	}
}

func TestCompile_NonBooleanTopLevelDitolak(t *testing.T) {
	// Ekspresi yang tipenya diketahui saat compile dan bukan boolean → ditolak.
	cases := []string{
		"actor.persona",   // string
		"'literal'",       // string
		"42",              // number
		"actor.tenant_id", // string
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			if _, err := workflow.Compile(expr); err == nil {
				t.Fatalf("Compile(%q) harusnya menolak hasil non-boolean", expr)
			}
		})
	}
}

func TestCompile_TypeMismatchComparison(t *testing.T) {
	cases := []string{
		"actor.persona > 3",    // string vs number ordering
		"actor.persona == 5",   // string == number
		"actor.is_citizen > 1", // bool ordering
		"'a' >= 'b'",           // string ordering
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			if _, err := workflow.Compile(expr); err == nil {
				t.Fatalf("Compile(%q) harusnya menolak perbandingan tak koheren", expr)
			}
		})
	}
}

func TestCompile_ValidExpressions(t *testing.T) {
	cases := []string{
		"actor.has_permission('surat_masuk:surat:disposisi')",
		"actor.has_role('pimpinan_opd')",
		"actor.has_central_role('super_admin')",
		"actor.is_citizen",
		"!actor.is_cross_tenant",
		"actor.persona == 'employee'",
		"actor.employment_status != 'non_asn'",
		"entity.nilai > 100",
		"entity.status == 'diajukan' && actor.has_role('verifikator')",
		"entity.catatan != ''",
		"(actor.is_citizen || actor.has_role('x')) && entity.nilai <= 500",
		"entity.approved", // typeAny top-level — boleh, dicek saat eval
		"actor.has_permission('a') && !actor.has_role('b') || entity.n >= 10",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			if _, err := workflow.Compile(expr); err != nil {
				t.Fatalf("Compile(%q) diharapkan sukses, dapat: %v", expr, err)
			}
		})
	}
}

// ===== Evaluate: hasil boolean benar =====

func evalGuard(t *testing.T, expr string, actor port.AuthContext, entity map[string]any) bool {
	t.Helper()
	prog, err := workflow.Compile(expr)
	if err != nil {
		t.Fatalf("Compile(%q): %v", expr, err)
	}
	got, err := prog.Eval(actor, entity)
	if err != nil {
		t.Fatalf("Eval(%q): %v", expr, err)
	}
	return got
}

func TestEval_ActorPermission(t *testing.T) {
	actor := testkit.Ctx(t, testkit.WithPermission("surat_masuk:surat:disposisi"))
	if !evalGuard(t, "actor.has_permission('surat_masuk:surat:disposisi')", actor, nil) {
		t.Fatal("permission dimiliki tapi guard false")
	}
	if evalGuard(t, "actor.has_permission('surat_masuk:surat:hapus')", actor, nil) {
		t.Fatal("permission tidak dimiliki tapi guard true")
	}
}

func TestEval_ActorRoleDanProps(t *testing.T) {
	actor := testkit.Ctx(t, testkit.WithRole("pimpinan_opd"))
	if !evalGuard(t, "actor.has_role('pimpinan_opd')", actor, nil) {
		t.Fatal("role dimiliki tapi guard false")
	}
	// TestContext: persona=employee, employment_status=asn, is_citizen=false.
	if !evalGuard(t, "actor.persona == 'employee'", actor, nil) {
		t.Fatal("persona employee harusnya cocok")
	}
	if evalGuard(t, "actor.is_citizen", actor, nil) {
		t.Fatal("is_citizen harusnya false")
	}
	if !evalGuard(t, "!actor.is_citizen", actor, nil) {
		t.Fatal("!is_citizen harusnya true")
	}
}

func TestEval_EntityComparisons(t *testing.T) {
	actor := testkit.Ctx(t)
	cases := []struct {
		expr   string
		entity map[string]any
		want   bool
	}{
		{"entity.nilai > 100", map[string]any{"nilai": 150}, true},
		{"entity.nilai > 100", map[string]any{"nilai": 50}, false},
		{"entity.nilai >= 100", map[string]any{"nilai": 100}, true},
		{"entity.nilai < 100", map[string]any{"nilai": 99.9}, true},
		{"entity.nilai <= 100", map[string]any{"nilai": int64(100)}, true},
		{"entity.status == 'diajukan'", map[string]any{"status": "diajukan"}, true},
		{"entity.status == 'diajukan'", map[string]any{"status": "ditolak"}, false},
		{"entity.catatan != ''", map[string]any{"catatan": "ada isi"}, true},
		{"entity.catatan != ''", map[string]any{"catatan": ""}, false},
		{"entity.nilai == 100", map[string]any{"nilai": 100.0}, true}, // int-literal vs float value
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := evalGuard(t, c.expr, actor, c.entity); got != c.want {
				t.Fatalf("Eval(%q, %v) = %v, mau %v", c.expr, c.entity, got, c.want)
			}
		})
	}
}

func TestEval_BooleanOperatorsShortCircuit(t *testing.T) {
	actor := testkit.Ctx(t, testkit.WithRole("verifikator"))
	entity := map[string]any{"nilai": 500}

	if !evalGuard(t, "actor.has_role('verifikator') && entity.nilai == 500", actor, entity) {
		t.Fatal("AND kedua benar harusnya true")
	}
	if evalGuard(t, "actor.has_role('bukan') && entity.nilai == 500", actor, entity) {
		t.Fatal("AND salah-satu salah harusnya false")
	}
	if !evalGuard(t, "actor.has_role('bukan') || entity.nilai == 500", actor, entity) {
		t.Fatal("OR salah-satu benar harusnya true")
	}
	if !evalGuard(t, "(actor.is_citizen || actor.has_role('verifikator')) && entity.nilai <= 500", actor, entity) {
		t.Fatal("grup + AND harusnya true")
	}
}

func TestEval_EntityMissingField(t *testing.T) {
	actor := testkit.Ctx(t)
	// Field tak ada → nil. nil != '' → true; nil == '' → false.
	if !evalGuard(t, "entity.catatan != ''", actor, map[string]any{}) {
		t.Fatal("field hilang != '' harusnya true")
	}
	if evalGuard(t, "entity.catatan == ''", actor, map[string]any{}) {
		t.Fatal("field hilang == '' harusnya false")
	}
	// Entity nil map juga aman.
	if !evalGuard(t, "entity.apa != ''", actor, nil) {
		t.Fatal("entity nil, field != '' harusnya true")
	}
}

// ===== Evaluate: error runtime =====

func TestEval_NonBooleanEntityDitolakSaatRuntime(t *testing.T) {
	actor := testkit.Ctx(t)
	prog, err := workflow.Compile("entity.approved") // typeAny lolos compile
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// Field bernilai angka → bukan boolean → error saat eval.
	if _, err := prog.Eval(actor, map[string]any{"approved": 7}); err == nil {
		t.Fatal("Eval nilai non-boolean harusnya error")
	}
	// Field bernilai boolean → sukses.
	got, err := prog.Eval(actor, map[string]any{"approved": true})
	if err != nil {
		t.Fatalf("Eval boolean entity: %v", err)
	}
	if !got {
		t.Fatal("entity.approved=true harusnya true")
	}
}

func TestEval_OrderingNonNumericRuntime(t *testing.T) {
	actor := testkit.Ctx(t)
	// entity.x string dibandingkan > angka → tipe tak diketahui saat compile (typeAny),
	// tapi error saat runtime karena string bukan numerik.
	prog, err := workflow.Compile("entity.x > 3")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = prog.Eval(actor, map[string]any{"x": "bukan angka"})
	if err == nil {
		t.Fatal("perbandingan ordering pada string harusnya error runtime")
	}
	// Error runtime melampirkan ekspresi sumber untuk memudahkan diagnosa.
	if !strings.Contains(err.Error(), "entity.x > 3") {
		t.Fatalf("error runtime tak menyebut ekspresi sumber: %v", err)
	}
}

func TestEval_NegativeNumberLiteral(t *testing.T) {
	actor := testkit.Ctx(t)
	if !evalGuard(t, "entity.selisih < -50000", actor, map[string]any{"selisih": -60000}) {
		t.Fatal("-60000 < -50000 harusnya true")
	}
	if evalGuard(t, "entity.selisih < -50000", actor, map[string]any{"selisih": -40000}) {
		t.Fatal("-40000 < -50000 harusnya false")
	}
	if !evalGuard(t, "entity.saldo == -100", actor, map[string]any{"saldo": -100}) {
		t.Fatal("saldo == -100 harusnya true")
	}
	// '-' tanpa angka setelahnya → ditolak saat compile.
	if _, err := workflow.Compile("entity.x > -"); err == nil {
		t.Fatal("'-' tanpa angka harusnya error compile")
	}
}

// ===== Validate hook: guard invalid menolak definisi saat load =====

func TestValidate_GuardInvalidMenolakDefinisi(t *testing.T) {
	def := defDisposisi
	def.Transitions = append([]workflow.Transition(nil), defDisposisi.Transitions...)
	def.Transitions[0].Guards = []string{"actor.has_permission("} // syntax error

	if err := workflow.Validate(def); err == nil {
		t.Fatal("Validate harusnya menolak definisi dengan guard invalid")
	}

	store := workflow.NewMemoryStore()
	if err := store.Register(def); err == nil {
		t.Fatal("Register harusnya menolak definisi dengan guard invalid")
	}
}

func TestValidate_GuardKosongDitolak(t *testing.T) {
	def := defDisposisi
	def.Transitions = append([]workflow.Transition(nil), defDisposisi.Transitions...)
	def.Transitions[0].Guards = []string{"   "}

	if err := workflow.Validate(def); err == nil {
		t.Fatal("Validate harusnya menolak guard whitespace kosong")
	}
}

func TestValidate_GuardValidLolos(t *testing.T) {
	// defDisposisi punya guard actor.has_permission(...) yang valid.
	if err := workflow.Validate(defDisposisi); err != nil {
		t.Fatalf("definisi dengan guard valid harusnya lolos: %v", err)
	}
}

// ===== DSLGuardEvaluator: dipakai engine, cache konsisten =====

func TestDSLGuardEvaluator_EvaluateDanCache(t *testing.T) {
	ev := workflow.NewGuardEvaluator()
	actor := testkit.Ctx(t, testkit.WithPermission("m:e:a"))

	ok, err := ev.Evaluate("actor.has_permission('m:e:a')", actor, nil)
	if err != nil || !ok {
		t.Fatalf("Evaluate pertama: ok=%v err=%v", ok, err)
	}
	// Panggilan kedua (dari cache) harus konsisten.
	ok, err = ev.Evaluate("actor.has_permission('m:e:a')", actor, nil)
	if err != nil || !ok {
		t.Fatalf("Evaluate cache: ok=%v err=%v", ok, err)
	}
	// Ekspresi invalid → error.
	if _, err := ev.Evaluate("actor.has_permission(", actor, nil); err == nil {
		t.Fatal("Evaluate ekspresi invalid harusnya error")
	}
}

func TestDSLGuardEvaluator_IntegrasiEngine(t *testing.T) {
	// Guard nyata via evaluator DSL: actor tanpa permission → transisi ditolak.
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register: %v", err)
	}
	dispatch := &dispatchRecord{}
	eng := workflow.New(store, dispatch, workflow.NewGuardEvaluator())

	actorTanpaPerm := testkit.Ctx(t)
	inst, err := eng.Start(actorTanpaPerm, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	err = eng.Execute(actorTanpaPerm, inst, "disposisi", nil)
	if err == nil {
		t.Fatal("transisi tanpa permission harusnya ditolak guard")
	}
	if !strings.Contains(err.Error(), "guard") && !strings.Contains(err.Error(), "workflow.guard") {
		t.Fatalf("error tak menyebut guard: %v", err)
	}
	if len(dispatch.called) != 0 {
		t.Fatal("action tidak boleh dipanggil saat guard gagal")
	}

	// Actor dengan permission → transisi lolos.
	actorBerhak := testkit.Ctx(t, testkit.WithPermission("surat_masuk:surat:disposisi"))
	inst2, _ := eng.Start(actorBerhak, defDisposisi.ID, uuid.New())
	if err := eng.Execute(actorBerhak, inst2, "disposisi", nil); err != nil {
		t.Fatalf("transisi dengan permission harusnya lolos: %v", err)
	}
	if inst2.CurrentState != "didisposisi" {
		t.Fatalf("state = %q, mau didisposisi", inst2.CurrentState)
	}
}
