package permission

import (
	"context"

	"github.com/google/uuid"
)

// GrantResolver mengembalikan scoped-grant seorang user pada SATU tenant. Diimplementasi di
// adapter tenant DB — `tenantrole/adapter/db.TenantScopedGrantResolver` (jangkauan unit dari
// assignment role) dan `delegation/adapter/db.DelegationScopedGrantResolver` (pelimpahan aktif).
// Port di sini, bukan di port/, karena Grant adalah tipe core: yang menyeberangi batas layer
// adalah port.ScopedEvaluator (hasil Bind), bukan bahan bakunya.
type GrantResolver interface {
	Grants(ctx context.Context, userID uuid.UUID) ([]Grant, error)
}

// CentralGrants menerjemahkan role SENTRAL yang dipegang actor menjadi Grant ber-jangkauan
// TenantWide. Ini emitter yang sengaja tidak dibuat di PR-2.3.5 dan menjadi bagian terakhir
// Authority (PR-W3b).
//
// Mengapa TenantWide: role sentral tidak punya konsep unit kerja — ia diberikan admin platform
// untuk lintas/seluruh tenant, dan yang membatasinya adalah tenant_scope, bukan unit. Scope
// teritorial itu SUDAH ditegakkan saat login (`scopedTokenMinter.mint` hanya membakar role sentral
// yang berlaku untuk tenant token), jadi setiap role sentral yang sampai ke sini memang berlaku
// penuh di tenant ini — persis alasan `port.AuthContext` tak diberi `TenantScope()` (ADR-019
// Keputusan 3). Menerjemahkannya jadi grant unit-scoped akan salah ke dua arah sekaligus:
// helpdesk kementerian mendadak butuh assignment unit, dan wewenang lintas-unit (mis. inspektorat,
// PRD core/permission §hierarki "edge matriks/lintas-unit") kehilangan satu-satunya cara dinyatakan.
//
// refs boleh memuat ref lapis tenant — ia diabaikan: yang dicari HANYA yang ber-origin central,
// dan resolusinya lewat LookupRef sehingga nama tenant tak bisa naik ke definisi sentral
// (ADR-019/B8). Nama yang tak terdaftar diabaikan (fail-closed: tak ada grant), sama seperti
// Engine memperlakukan role tak dikenal.
func CentralGrants(cat RefCatalog, refs []RoleRef) []Grant {
	if cat == nil {
		return nil
	}
	var out []Grant
	seen := make(map[Permission]bool)
	for _, ref := range refs {
		if ref.Origin != OriginCentral {
			continue
		}
		role, ok := cat.LookupRef(ref)
		if !ok {
			continue
		}
		for _, p := range role.Permissions {
			if seen[p] {
				continue // dua role sentral bisa memberi permission sama; satu grant cukup
			}
			seen[p] = true
			out = append(out, Grant{Permission: p, TenantWide: true})
		}
	}
	return out
}

// BuildAuthority merakit kewenangan efektif actor di satu tenant: Roles apa adanya dari klaim
// token, RoleGrants = grant sentral (TenantWide) ∪ grant assignment role tenant, DelegatedGrants
// dari delegasi aktif.
//
// Pemisahan tiga komponen dipertahankan persis seperti kontrak Authority: Roles tetap memuat
// SELURUH ref (termasuk yang tak memberi perm) karena strict-intersection membutuhkannya, dan
// DelegatedGrants tetap terpisah karena ia jalur mandiri yang tak tunduk pada resolusi role.
//
// roleGrants/delegated boleh nil (mis. konteks tanpa tenant): komponennya kosong, dan Authority
// tanpa grant berarti setiap AllowsInUnit menjawab TIDAK — fail-closed, bukan permisif.
func BuildAuthority(
	ctx context.Context,
	central RefCatalog,
	roleGrants, delegated GrantResolver,
	userID uuid.UUID,
	refs []RoleRef,
) (Authority, error) {
	auth := Authority{Roles: refs, RoleGrants: CentralGrants(central, refs)}

	if roleGrants != nil {
		g, err := roleGrants.Grants(ctx, userID)
		if err != nil {
			// Gagal membaca jangkauan = tak bisa memutuskan. Kembalikan error agar pemanggil
			// (middleware) menolak request, bukan melanjutkan dengan Authority yang bolong —
			// Authority bolong tidak terasa seperti kegagalan, ia terasa seperti "tidak berwenang"
			// bagi user yang sebenarnya berwenang, dan seperti lolos bila kelak ada default lain.
			return Authority{}, err
		}
		auth.RoleGrants = append(auth.RoleGrants, g...)
	}
	if delegated != nil {
		g, err := delegated.Grants(ctx, userID)
		if err != nil {
			return Authority{}, err
		}
		auth.DelegatedGrants = g
	}
	return auth, nil
}
