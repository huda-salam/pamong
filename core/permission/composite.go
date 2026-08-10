package permission

// CompositeCatalog menggabungkan katalog central dan tenant menjadi satu RefCatalog sehingga
// Engine melihat lapis central (global/scoped, snapshot proses) dan tenant (snapshot per-tenant)
// berbarengan tanpa mengubah kontrak RoleCatalog tiap adapter — penerapan titik ekstensi #1
// (Open/Closed). Dipakai saat wiring auth (2.4) untuk membangun evaluator per-tenant.
//
// Resolusi dikurung PER LAPIS ASAL (ADR-019): nama dari klaim tenant_roles dicari HANYA di
// katalog tenant, nama dari klaim central_roles HANYA di katalog central. Sebelumnya kedua
// katalog dicoba berurutan atas nama telanjang ("central dulu, yang pertama mengenali menang"),
// yang menutup arah SHADOW (tenant menurunkan role global ke LayerTenant) tapi membuka arah
// NAIK yang jauh lebih berbahaya: role tenant yang sengaja dinamai persis seperti role sentral
// me-resolve ke definisi sentral dan mewarisi LayerGlobal — menang tanpa syarat, melewati
// strict-intersection, dan membuka seluruh permission `identity:*` tanpa pernah menyebutnya
// (REVIEW_BACKLOG B8). Pengurungan per-lapis menutup KEDUA arah sekaligus: nama yang sama di
// dua lapis kini adalah dua role yang berbeda, persis seperti `realm_access` vs
// `resource_access` di Keycloak dan `Role` vs `ClusterRole` di Kubernetes.
type CompositeCatalog struct {
	central RoleCatalog
	tenant  RoleCatalog
}

var _ RefCatalog = (*CompositeCatalog)(nil)

// NewCompositeCatalog menggabungkan katalog central dan tenant. Keduanya boleh nil: citizen
// (tanpa tenant) hanya punya central, dan bootstrap awal bisa belum punya keduanya — ref ke
// lapis yang katalognya nil selalu tak ditemukan (fail-closed, role diabaikan).
//
// SENGAJA tidak variadic dan SENGAJA tidak mengimplementasi RoleCatalog: kedua bentuk itu
// menerima lookup nama telanjang, yaitu bentuk yang justru dihapus di sini. Kompilasi adalah
// penegakannya — jalur lama tak bisa dipanggil ulang tanpa terlihat.
func NewCompositeCatalog(central, tenant RoleCatalog) *CompositeCatalog {
	return &CompositeCatalog{central: central, tenant: tenant}
}

// LookupRef mengembalikan definisi role dari katalog lapis asal ref.
//
// Layer hasil DIJEPIT ke lapis asal: apa pun yang dilaporkan katalog tenant diperlakukan
// sebagai LayerTenant. Ini pertahanan berlapis, bukan pengulangan — TenantRoleCatalog hari ini
// memang selalu menulis LayerTenant, tapi jepitan ini membuat katalog tenant mana pun (adapter
// baru, cache, katalog uji) tak bisa menaikkan lapis walau salah menulisnya.
func (c *CompositeCatalog) LookupRef(ref RoleRef) (Role, bool) {
	switch ref.Origin {
	case OriginCentral:
		if c.central == nil {
			return Role{}, false
		}
		r, ok := c.central.Lookup(ref.Name)
		if !ok {
			return Role{}, false
		}
		if r.Layer == LayerTenant {
			// Katalog central tak boleh memasok definisi ber-lapis tenant; tolak alih-alih
			// menebak lapis mana yang dimaksud.
			return Role{}, false
		}
		return r, true
	case OriginTenant:
		if c.tenant == nil {
			return Role{}, false
		}
		r, ok := c.tenant.Lookup(ref.Name)
		if !ok {
			return Role{}, false
		}
		r.Layer = LayerTenant
		return r, true
	default:
		return Role{}, false
	}
}
