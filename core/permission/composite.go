package permission

// CompositeCatalog menggabungkan beberapa RoleCatalog menjadi satu sehingga Engine
// melihat lapis central (global/scoped, snapshot proses) dan tenant (snapshot
// per-tenant) berbarengan tanpa mengubah kontrak RoleCatalog — penerapan titik
// ekstensi #1 (Open/Closed). Dipakai saat wiring auth (2.4) untuk membangun evaluator
// per-tenant; di 2.3.3 sudah dipakai langsung pada test resolusi & integration.
//
// Lookup mencoba tiap catalog berurutan; catalog PERTAMA yang mengenali nama menang.
// Karena itu central WAJIB didahulukan dari tenant: tenant tidak boleh menutupi
// (shadow) role global/scoped yang kebetulan bernama sama dengan menurunkannya ke
// LayerTenant — hal yang akan melemahkan prioritas global.
type CompositeCatalog struct {
	catalogs []RoleCatalog
}

var _ RoleCatalog = (*CompositeCatalog)(nil)

// NewCompositeCatalog menggabungkan catalog terurut prioritas (yang lebih awal menang
// pada bentrok nama). Konvensi pemanggilan: central dulu, baru tenant.
func NewCompositeCatalog(catalogs ...RoleCatalog) *CompositeCatalog {
	return &CompositeCatalog{catalogs: catalogs}
}

// Lookup mengembalikan definisi role dari catalog pertama yang mengenalinya.
func (c *CompositeCatalog) Lookup(name string) (Role, bool) {
	for _, cat := range c.catalogs {
		if r, ok := cat.Lookup(name); ok {
			return r, true
		}
	}
	return Role{}, false
}
