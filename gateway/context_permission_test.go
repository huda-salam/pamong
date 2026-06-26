package gateway

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
)

// denyAll dan grantSet adalah stub port.PermissionEvaluator untuk menguji seam.
type stubEvaluator struct {
	granted map[string]bool
}

func (s stubEvaluator) Allows(_ []string, perm string) bool { return s.granted[perm] }

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
