package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/huda-salam/pamong/port"
)

// ===== Guard expression DSL (PR-3.2.5) =====
//
// DSL sempit, boolean-only, tanpa side-effect (CLAUDE.md §"Fleksibilitas" Mekanisme 3,
// PRD workflow F5). Ekspresi di-compile SEKALI saat definisi di-load (lewat Validate) →
// syntax error / root tak dikenal ditolak di pintu masuk, bukan saat runtime.
//
// Yang BISA dilakukan ekspresi:
//   - Baca konteks actor:  actor.has_permission('m:e:a'), actor.has_role('x'),
//                          actor.has_central_role('x'), actor.persona,
//                          actor.employment_status, actor.tenant_id,
//                          actor.is_citizen, actor.is_cross_tenant
//   - Baca field entity:   entity.<field>  (nilai dinamis dari snapshot dokumen)
//   - Operator boolean:    && || !
//   - Perbandingan:        == != > < >= <=
//   - Literal:             'string', angka, true, false
//   - Grouping:            ( ... )
//
// Yang TIDAK BISA (sengaja, demi auditabilitas > ekspresivitas):
//   - Memanggil fungsi arbitrary, akses I/O, loop, mutasi apapun.
//   - Fungsi custom yang didefinisikan tenant.
//
// Output ekspresi WAJIB boolean. Untuk ekspresi yang tipenya diketahui saat compile
// (perbandingan, &&/||/!, literal bool) non-boolean ditolak saat compile. Untuk yang
// bergantung pada nilai entity (typeAny), boolness dicek saat evaluate.

// valueType adalah tipe statis sederhana yang dilacak saat compile untuk type-checking.
// typeAny dipakai untuk field entity yang tipenya baru diketahui saat runtime.
type valueType int

const (
	typeAny valueType = iota
	typeBool
	typeString
	typeNumber
)

func (t valueType) String() string {
	switch t {
	case typeBool:
		return "boolean"
	case typeString:
		return "string"
	case typeNumber:
		return "number"
	default:
		return "any"
	}
}

// ===== AST =====

// evalScope membawa konteks read-only yang boleh dibaca ekspresi: actor dan entity.
// Tidak ada jalur menulis — guard bebas side-effect (PRD F5).
type evalScope struct {
	actor  port.AuthContext
	entity map[string]any
}

// node adalah simpul AST. typ() dipakai type-checker saat compile; eval() dipakai runtime.
type node interface {
	typ() valueType
	eval(s *evalScope) (any, error)
}

// --- literal ---

type boolLit struct{ v bool }

func (n boolLit) typ() valueType               { return typeBool }
func (n boolLit) eval(*evalScope) (any, error) { return n.v, nil }

type strLit struct{ v string }

func (n strLit) typ() valueType               { return typeString }
func (n strLit) eval(*evalScope) (any, error) { return n.v, nil }

type numLit struct{ v float64 }

func (n numLit) typ() valueType               { return typeNumber }
func (n numLit) eval(*evalScope) (any, error) { return n.v, nil }

// --- actor accessor ---

// actorProp membaca properti skalar actor (persona, tenant_id, is_citizen, dst).
type actorProp struct {
	name string
	t    valueType
}

func (n actorProp) typ() valueType { return n.t }
func (n actorProp) eval(s *evalScope) (any, error) {
	switch n.name {
	case "persona":
		return s.actor.Persona(), nil
	case "employment_status":
		return s.actor.EmploymentStatus(), nil
	case "tenant_id":
		return s.actor.TenantID(), nil
	case "is_citizen":
		return s.actor.IsCitizen(), nil
	case "is_cross_tenant":
		return s.actor.IsCrossTenant(), nil
	default:
		// Tidak mungkin tercapai: divalidasi saat parse.
		return nil, fmt.Errorf("properti actor tak dikenal: %s", n.name)
	}
}

// actorFunc membaca predikat boolean actor yang menerima satu argumen string.
type actorFunc struct {
	name string
	arg  string
}

func (n actorFunc) typ() valueType { return typeBool }
func (n actorFunc) eval(s *evalScope) (any, error) {
	switch n.name {
	case "has_permission":
		return s.actor.RequirePermission(n.arg) == nil, nil
	case "has_role":
		return s.actor.HasRole(n.arg), nil
	case "has_central_role":
		return s.actor.HasCentralRole(n.arg), nil
	default:
		return nil, fmt.Errorf("fungsi actor tak dikenal: %s", n.name)
	}
}

// entityField membaca satu field dari snapshot entity. Tipenya dinamis (typeAny) —
// field yang tidak ada bernilai nil.
type entityField struct{ name string }

func (n entityField) typ() valueType { return typeAny }
func (n entityField) eval(s *evalScope) (any, error) {
	if s.entity == nil {
		return nil, nil
	}
	return s.entity[n.name], nil
}

// --- operator ---

type notNode struct{ x node }

func (n notNode) typ() valueType { return typeBool }
func (n notNode) eval(s *evalScope) (any, error) {
	v, err := n.x.eval(s)
	if err != nil {
		return nil, err
	}
	b, err := asBool(v)
	if err != nil {
		return nil, err
	}
	return !b, nil
}

// logicNode menangani && dan || dengan short-circuit.
type logicNode struct {
	or   bool // true = ||, false = &&
	l, r node
}

func (n logicNode) typ() valueType { return typeBool }
func (n logicNode) eval(s *evalScope) (any, error) {
	lv, err := n.l.eval(s)
	if err != nil {
		return nil, err
	}
	lb, err := asBool(lv)
	if err != nil {
		return nil, err
	}
	// Short-circuit: || sudah true, && sudah false.
	if n.or && lb {
		return true, nil
	}
	if !n.or && !lb {
		return false, nil
	}
	rv, err := n.r.eval(s)
	if err != nil {
		return nil, err
	}
	return asBool(rv)
}

// cmpNode menangani == != > < >= <=.
type cmpNode struct {
	op   string
	l, r node
}

func (n cmpNode) typ() valueType { return typeBool }
func (n cmpNode) eval(s *evalScope) (any, error) {
	lv, err := n.l.eval(s)
	if err != nil {
		return nil, err
	}
	rv, err := n.r.eval(s)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return equalValues(lv, rv), nil
	case "!=":
		return !equalValues(lv, rv), nil
	default: // > < >= <= — hanya numerik
		lf, ok1 := toFloat(lv)
		rf, ok2 := toFloat(rv)
		if !ok1 || !ok2 {
			// Error mentah — Program.Eval melampirkan ekspresi sumbernya.
			return nil, fmt.Errorf(
				"operator %q butuh operand numerik, dapat %s dan %s",
				n.op, describeValue(lv), describeValue(rv))
		}
		switch n.op {
		case ">":
			return lf > rf, nil
		case "<":
			return lf < rf, nil
		case ">=":
			return lf >= rf, nil
		case "<=":
			return lf <= rf, nil
		}
	}
	return nil, fmt.Errorf("operator perbandingan tak dikenal: %s", n.op)
}

// ===== Program =====

// Program adalah ekspresi guard yang sudah ter-compile. Eval bebas side-effect dan
// aman dipanggil konkuren.
type Program struct {
	src  string
	root node
}

// Eval mengevaluasi ekspresi terhadap actor + entity, menghasilkan boolean.
// Nilai non-boolean (mis. `entity.jumlah` yang ternyata angka) → error, memenuhi
// invariant "guard harus boolean" pada jalur yang tipenya tak diketahui saat compile.
func (p *Program) Eval(actor port.AuthContext, entity map[string]any) (bool, error) {
	v, err := p.root.eval(&evalScope{actor: actor, entity: entity})
	if err != nil {
		return false, ErrInvalidGuard(p.src, err.Error())
	}
	b, err := asBool(v)
	if err != nil {
		return false, ErrInvalidGuard(p.src, err.Error())
	}
	return b, nil
}

// Compile mem-parsing dan type-check satu ekspresi guard. Dipanggil saat load —
// syntax error, root tak dikenal, atau tipe hasil non-boolean ditolak di sini.
func Compile(expr string) (*Program, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, ErrInvalidGuard(expr, err.Error())
	}
	p := &parser{toks: toks}
	root, err := p.parseExpr()
	if err != nil {
		return nil, ErrInvalidGuard(expr, err.Error())
	}
	if p.cur().kind != tokEOF {
		return nil, ErrInvalidGuard(expr, fmt.Sprintf("token tak terduga %q setelah ekspresi", p.cur().text))
	}
	// Hasil top-level harus boolean. typeAny (mis. `entity.approved`) dibiarkan lolos
	// compile dan dicek saat Eval — tipenya belum diketahui tanpa data.
	if t := root.typ(); t != typeBool && t != typeAny {
		return nil, ErrInvalidGuard(expr, fmt.Sprintf("ekspresi guard harus menghasilkan boolean, bukan %s", t))
	}
	return &Program{src: expr, root: root}, nil
}

// ===== Lexer =====

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokString
	tokNumber
	tokDot
	tokLParen
	tokRParen
	tokComma
	tokMinus
	tokAnd
	tokOr
	tokNot
	tokEq
	tokNeq
	tokGt
	tokLt
	tokGte
	tokLte
)

type token struct {
	kind tokKind
	text string
}

func lex(src string) ([]token, error) {
	var toks []token
	rs := []rune(src)
	i := 0
	for i < len(rs) {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tokLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tokRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, token{tokComma, ","})
			i++
		case c == '-':
			toks = append(toks, token{tokMinus, "-"})
			i++
		case c == '.':
			toks = append(toks, token{tokDot, "."})
			i++
		case c == '&':
			if i+1 < len(rs) && rs[i+1] == '&' {
				toks = append(toks, token{tokAnd, "&&"})
				i += 2
			} else {
				return nil, fmt.Errorf("karakter '&' tunggal tidak valid (maksud '&&'?)")
			}
		case c == '|':
			if i+1 < len(rs) && rs[i+1] == '|' {
				toks = append(toks, token{tokOr, "||"})
				i += 2
			} else {
				return nil, fmt.Errorf("karakter '|' tunggal tidak valid (maksud '||'?)")
			}
		case c == '!':
			if i+1 < len(rs) && rs[i+1] == '=' {
				toks = append(toks, token{tokNeq, "!="})
				i += 2
			} else {
				toks = append(toks, token{tokNot, "!"})
				i++
			}
		case c == '=':
			if i+1 < len(rs) && rs[i+1] == '=' {
				toks = append(toks, token{tokEq, "=="})
				i += 2
			} else {
				return nil, fmt.Errorf("karakter '=' tunggal tidak valid (maksud '=='?)")
			}
		case c == '>':
			if i+1 < len(rs) && rs[i+1] == '=' {
				toks = append(toks, token{tokGte, ">="})
				i += 2
			} else {
				toks = append(toks, token{tokGt, ">"})
				i++
			}
		case c == '<':
			if i+1 < len(rs) && rs[i+1] == '=' {
				toks = append(toks, token{tokLte, "<="})
				i += 2
			} else {
				toks = append(toks, token{tokLt, "<"})
				i++
			}
		case c == '\'':
			// String literal — sampai kutip penutup. Tidak ada escape (DSL sempit).
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("string literal tidak ditutup")
			}
			toks = append(toks, token{tokString, string(rs[i+1 : j])})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			dot := false
			for j < len(rs) && (rs[j] >= '0' && rs[j] <= '9' || rs[j] == '.') {
				if rs[j] == '.' {
					// Titik desimal hanya sekali; titik kedua adalah member access.
					if dot {
						break
					}
					// Pastikan digit setelah titik agar `entity.5`-like tak terjadi.
					if j+1 >= len(rs) || rs[j+1] < '0' || rs[j+1] > '9' {
						break
					}
					dot = true
				}
				j++
			}
			toks = append(toks, token{tokNumber, string(rs[i:j])})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(rs) && isIdentPart(rs[j]) {
				j++
			}
			toks = append(toks, token{tokIdent, string(rs[i:j])})
			i = j
		default:
			return nil, fmt.Errorf("karakter tak dikenal %q", string(c))
		}
	}
	toks = append(toks, token{tokEOF, ""})
	return toks, nil
}

func isIdentStart(c rune) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c rune) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

// ===== Parser (recursive descent) =====
//
// Presedensi (rendah→tinggi): || , && , perbandingan , unary ! , primary.

type parser struct {
	toks []token
	pos  int
}

func (p *parser) cur() token { return p.toks[p.pos] }
func (p *parser) advance()   { p.pos++ }

func (p *parser) parseExpr() (node, error) { return p.parseOr() }

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = logicNode{or: true, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokAnd {
		p.advance()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = logicNode{or: false, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseComparison() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	op, isCmp := cmpOp(p.cur().kind)
	if !isCmp {
		return left, nil
	}
	p.advance()
	right, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	// Type-check operand perbandingan saat compile bila tipe diketahui.
	if err := checkComparable(op, left, right); err != nil {
		return nil, err
	}
	return cmpNode{op: op, l: left, r: right}, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.cur().kind == tokNot {
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if t := x.typ(); t != typeBool && t != typeAny {
			return nil, fmt.Errorf("operator '!' butuh boolean, dapat %s", t)
		}
		return notNode{x: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	tok := p.cur()
	switch tok.kind {
	case tokLParen:
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tokRParen {
			return nil, fmt.Errorf("kurung tutup ')' diharapkan")
		}
		p.advance()
		return inner, nil
	case tokString:
		p.advance()
		return strLit{v: tok.text}, nil
	case tokMinus:
		// Negasi HANYA pada literal angka (mis. -100) — bukan aritmatika umum.
		p.advance()
		if p.cur().kind != tokNumber {
			return nil, fmt.Errorf("'-' hanya boleh mendahului literal angka")
		}
		numTok := p.cur()
		p.advance()
		f, err := strconv.ParseFloat(numTok.text, 64)
		if err != nil {
			return nil, fmt.Errorf("angka tidak valid %q", numTok.text)
		}
		return numLit{v: -f}, nil
	case tokNumber:
		p.advance()
		f, err := strconv.ParseFloat(tok.text, 64)
		if err != nil {
			return nil, fmt.Errorf("angka tidak valid %q", tok.text)
		}
		return numLit{v: f}, nil
	case tokIdent:
		return p.parseIdent()
	default:
		if tok.kind == tokEOF {
			return nil, fmt.Errorf("ekspresi tidak lengkap")
		}
		return nil, fmt.Errorf("token tak terduga %q", tok.text)
	}
}

// parseIdent menangani keyword bool (true/false) dan accessor ber-root (actor.x, entity.x).
// Tidak ada variabel bebas — root selain 'actor'/'entity' ditolak (DSL sempit).
func (p *parser) parseIdent() (node, error) {
	root := p.cur().text
	switch root {
	case "true":
		p.advance()
		return boolLit{v: true}, nil
	case "false":
		p.advance()
		return boolLit{v: false}, nil
	case "actor", "entity":
		p.advance()
		if p.cur().kind != tokDot {
			return nil, fmt.Errorf("'%s' harus diikuti '.member'", root)
		}
		p.advance()
		if p.cur().kind != tokIdent {
			return nil, fmt.Errorf("nama member diharapkan setelah '%s.'", root)
		}
		member := p.cur().text
		p.advance()
		if root == "actor" {
			return p.buildActorAccess(member)
		}
		return entityField{name: member}, nil
	default:
		return nil, fmt.Errorf("root tak dikenal %q (hanya 'actor' dan 'entity' yang diperbolehkan)", root)
	}
}

// buildActorAccess membangun node untuk actor.<member>, membedakan properti skalar
// dari fungsi predikat (yang diikuti '(...)').
func (p *parser) buildActorAccess(member string) (node, error) {
	if p.cur().kind == tokLParen {
		// Fungsi: actor.has_permission('x') / has_role / has_central_role.
		if !isActorFunc(member) {
			return nil, fmt.Errorf("fungsi actor tak dikenal %q", member)
		}
		p.advance()
		if p.cur().kind != tokString {
			return nil, fmt.Errorf("actor.%s butuh satu argumen string literal", member)
		}
		arg := p.cur().text
		p.advance()
		if p.cur().kind != tokRParen {
			return nil, fmt.Errorf("kurung tutup ')' diharapkan untuk actor.%s", member)
		}
		p.advance()
		return actorFunc{name: member, arg: arg}, nil
	}
	// Properti skalar.
	t, ok := actorPropType(member)
	if !ok {
		return nil, fmt.Errorf("properti actor tak dikenal %q", member)
	}
	return actorProp{name: member, t: t}, nil
}

func isActorFunc(name string) bool {
	switch name {
	case "has_permission", "has_role", "has_central_role":
		return true
	}
	return false
}

// actorPropType mengembalikan tipe statis properti actor skalar.
func actorPropType(name string) (valueType, bool) {
	switch name {
	case "persona", "employment_status", "tenant_id":
		return typeString, true
	case "is_citizen", "is_cross_tenant":
		return typeBool, true
	}
	return typeAny, false
}

func cmpOp(k tokKind) (string, bool) {
	switch k {
	case tokEq:
		return "==", true
	case tokNeq:
		return "!=", true
	case tokGt:
		return ">", true
	case tokLt:
		return "<", true
	case tokGte:
		return ">=", true
	case tokLte:
		return "<=", true
	}
	return "", false
}

// checkComparable menolak perbandingan yang jelas tidak koheren saat compile bila kedua
// tipe diketahui. Operand typeAny (field entity) dilewati — dicek saat runtime.
func checkComparable(op string, l, r node) error {
	lt, rt := l.typ(), r.typ()
	if lt == typeAny || rt == typeAny {
		return nil
	}
	switch op {
	case "==", "!=":
		if lt != rt {
			return fmt.Errorf("tidak bisa membandingkan %s dengan %s pakai %q", lt, rt, op)
		}
	default: // > < >= <=
		if lt != typeNumber || rt != typeNumber {
			return fmt.Errorf("operator %q hanya untuk number, dapat %s dan %s", op, lt, rt)
		}
	}
	return nil
}

// ===== Runtime value helpers =====

// asBool mengonversi hasil eval ke boolean, error bila bukan boolean.
func asBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("nilai %s bukan boolean", describeValue(v))
	}
	return b, nil
}

// toFloat menormalkan tipe numerik Go apapun (int/uint/float dari entity map) ke float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// equalValues membandingkan kesetaraan dua nilai lintas tipe secara aman.
// Angka dibandingkan numerik (int vs float64), sisanya per-tipe; nil hanya sama dengan nil.
func equalValues(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
		return false
	}
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	}
	return false
}

func describeValue(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case bool:
		return "boolean"
	case string:
		return "string"
	default:
		if _, ok := toFloat(v); ok {
			return "number"
		}
		return fmt.Sprintf("%T", v)
	}
}

// ===== GuardEvaluator implementation =====

// DSLGuardEvaluator adalah implementasi GuardEvaluator berbasis DSL ini. Ia meng-cache
// Program ter-compile per ekspresi sehingga Evaluate di jalur runtime tidak mem-parsing
// ulang (memenuhi target < 5ms, PRD NFR). Aman dipanggil konkuren.
type DSLGuardEvaluator struct {
	cache sync.Map // string(expr) -> *Program
}

var _ GuardEvaluator = (*DSLGuardEvaluator)(nil)

// NewGuardEvaluator membuat evaluator DSL siap pakai.
func NewGuardEvaluator() *DSLGuardEvaluator { return &DSLGuardEvaluator{} }

// Evaluate meng-compile (sekali, lalu cache) dan mengevaluasi ekspresi.
// Idealnya ekspresi sudah ter-compile saat load (Validate); di sini compile hanya
// fallback bila evaluator dipakai tanpa melewati jalur load.
func (e *DSLGuardEvaluator) Evaluate(expr string, actor port.AuthContext, entity map[string]any) (bool, error) {
	prog, err := e.compile(expr)
	if err != nil {
		return false, err
	}
	return prog.Eval(actor, entity)
}

func (e *DSLGuardEvaluator) compile(expr string) (*Program, error) {
	if v, ok := e.cache.Load(expr); ok {
		return v.(*Program), nil
	}
	prog, err := Compile(expr)
	if err != nil {
		return nil, err
	}
	e.cache.Store(expr, prog)
	return prog, nil
}

// ===== Load-time validation hook =====

// validateGuards meng-compile setiap guard di semua transisi definisi. Dipanggil dari
// Validate agar syntax error / tipe non-boolean ketahuan saat load, bukan runtime
// (PRD F5, CLAUDE.md §"Fleksibilitas" Mekanisme 3).
func validateGuards(def WorkflowDefinition) error {
	for i, tr := range def.Transitions {
		for _, expr := range tr.Guards {
			if strings.TrimSpace(expr) == "" {
				return ErrInvalidDefinition(fmt.Sprintf("transisi[%d]: guard kosong tidak diperbolehkan", i))
			}
			if _, err := Compile(expr); err != nil {
				return err
			}
		}
	}
	return nil
}
