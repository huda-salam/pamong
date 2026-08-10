package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// denyAll dan grantSet adalah stub port.PermissionEvaluator untuk menguji seam.
type stubEvaluator struct {
	granted map[string]bool
}

func (s stubEvaluator) Allows(_ []port.RoleRef, perm string) bool { return s.granted[perm] }

func TestContext_RequirePermission_NilEvaluatorPermissive(t *testing.T) {
	c := &Context{}
	if err := c.RequirePermission("surat_masuk:surat:buat"); err != nil {
		t.Fatalf("tanpa evaluator harus permisif, dapat error: %v", err)
	}
}

func TestContext_RequirePermission_Allowed(t *testing.T) {
	c := &Context{
		roles: map[string]bool{"operator_surat": true},
		eval:  stubEvaluator{granted: map[string]bool{"surat_masuk:surat:buat": true}},
	}
	if err := c.RequirePermission("surat_masuk:surat:buat"); err != nil {
		t.Fatalf("evaluator mengizinkan, harusnya nil, dapat: %v", err)
	}
}

func TestContext_RequirePermission_Denied(t *testing.T) {
	c := &Context{
		roles: map[string]bool{"operator_surat": true},
		eval:  stubEvaluator{granted: map[string]bool{}},
	}
	err := c.RequirePermission("surat_masuk:surat:disposisi")
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("evaluator menolak, mau PERMISSION_DENIED, dapat: %v", err)
	}
}

// --- B8 / ADR-019: lapis asal role dibawa dari klaim sampai ke evaluator ---

// recordingEvaluator menangkap ref yang benar-benar diterima evaluator.
type recordingEvaluator struct{ seen []port.RoleRef }

func (r *recordingEvaluator) Allows(roles []port.RoleRef, _ string) bool {
	r.seen = append(r.seen, roles...)
	return false
}

// TestContext_RoleListCarriesOrigin mengunci titik tempat cacat B8 lahir: NewContextFromClaims
// dulu MERATAKAN TenantRoles+CentralRoles jadi satu daftar nama telanjang, dan sesudah itu tak
// ada lagi yang bisa membedakan role tenant bernama "super_admin" dari role sentral bernama
// sama. JWT sudah memisahkan kedua klaim; di sinilah pemisahan itu wajib bertahan.
func TestContext_RoleListCarriesOrigin(t *testing.T) {
	rec := &recordingEvaluator{}
	c := NewContextFromClaims(context.Background(), &port.Claims{
		PersonID:     uuid.New(),
		Persona:      "employee",
		TenantID:     "pemkot-surabaya",
		TenantRoles:  []string{"super_admin"}, // nama yang sengaja menyamar
		CentralRoles: []string{"platform_helpdesk"},
	})
	c.SetPermissionEvaluator(rec)
	_ = c.RequirePermission("identity:tenant:nonaktif")

	want := map[port.RoleRef]bool{
		{Origin: port.RoleOriginTenant, Name: "super_admin"}:        true,
		{Origin: port.RoleOriginCentral, Name: "platform_helpdesk"}: true,
	}
	if len(rec.seen) != len(want) {
		t.Fatalf("evaluator menerima %d ref, mau %d: %+v", len(rec.seen), len(want), rec.seen)
	}
	for _, ref := range rec.seen {
		if !want[ref] {
			t.Errorf("ref tak terduga (lapis asal hilang atau tertukar): %+v", ref)
		}
	}
}

// TestContext_RoleListLazyCarriesOrigin: jalur konstruksi lain (FromRequest fallback, Context
// yang dirakit langsung di test) menghitung daftar ref secara lazy — ia harus membawa origin
// yang sama, bukan default diam-diam.
func TestContext_RoleListLazyCarriesOrigin(t *testing.T) {
	rec := &recordingEvaluator{}
	c := &Context{
		roles:        map[string]bool{"operator_surat": true},
		centralRoles: map[string]bool{"super_admin": true},
		eval:         rec,
	}
	_ = c.RequirePermission("x:y:z")

	got := map[string]port.RoleOrigin{}
	for _, ref := range rec.seen {
		got[ref.Name] = ref.Origin
	}
	if got["operator_surat"] != port.RoleOriginTenant {
		t.Errorf("role tenant harus ber-origin tenant, dapat %v", got["operator_surat"])
	}
	if got["super_admin"] != port.RoleOriginCentral {
		t.Errorf("role central harus ber-origin central, dapat %v", got["super_admin"])
	}
}
