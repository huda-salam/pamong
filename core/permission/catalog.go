package permission

// RoleCatalog memetakan nama role ke definisinya. PR-2.3.1 menyediakan implementasi
// in-memory (MemoryCatalog). PR-2.3.2/2.3.3 menambah implementasi berbasis DB
// (central di id.*, tenant di gov.*) tanpa mengubah Engine — penerapan
// titik ekstensi #1 (registry pattern, Open/Closed) pada CLAUDE.md.
type RoleCatalog interface {
	// Lookup mengembalikan definisi role dan true bila terdaftar.
	Lookup(name string) (Role, bool)
}

// RefCatalog me-resolve RoleRef — nama BESERTA lapis asalnya — ke definisi role. Inilah
// kontrak yang dipakai Engine sejak ADR-019; RoleCatalog (lookup nama telanjang) tetap ada
// sebagai kontrak satu-lapis yang diimplementasi tiap adapter katalog, tapi tidak lagi cukup
// untuk mengambil keputusan otorisasi: nama telanjang kehilangan lapis asal, dan di situlah
// role tenant bisa naik ke definisi sentral (REVIEW_BACKLOG B8).
type RefCatalog interface {
	// LookupRef mengembalikan definisi role untuk ref dan true bila terdaftar PADA LAPIS
	// ASAL ref itu. Nama yang hanya ada di lapis lain WAJIB tidak ditemukan.
	LookupRef(ref RoleRef) (Role, bool)
}

// MemoryCatalog adalah RoleCatalog in-memory untuk bootstrap awal & test.
type MemoryCatalog struct {
	roles map[string]Role
}

var _ RoleCatalog = (*MemoryCatalog)(nil)

// NewMemoryCatalog membuat katalog kosong.
func NewMemoryCatalog() *MemoryCatalog {
	return &MemoryCatalog{roles: make(map[string]Role)}
}

// Define mendaftarkan role beserta permission yang diberikannya. Chainable;
// pendaftaran ulang nama yang sama menimpa definisi sebelumnya.
func (c *MemoryCatalog) Define(name string, layer Layer, grants ...Permission) *MemoryCatalog {
	c.roles[name] = Role{Name: name, Layer: layer, Permissions: grants}
	return c
}

// Lookup mengembalikan definisi role dan true bila terdaftar.
func (c *MemoryCatalog) Lookup(name string) (Role, bool) {
	r, ok := c.roles[name]
	return r, ok
}

var _ RefCatalog = (*MemoryCatalog)(nil)

// LookupRef me-resolve ref dengan menghormati lapis asalnya. MemoryCatalog memuat kedua lapis
// dalam satu map, jadi origin dicocokkan terhadap Layer definisi yang ditemukan: ref tenant
// hanya boleh mendapat role ber-LayerTenant, ref central hanya role global/scoped. Tanpa
// pencocokan ini, katalog memori akan mengizinkan persis eskalasi yang ditutup ADR-019 —
// dan katalog inilah yang dipakai test & bootstrap awal.
func (c *MemoryCatalog) LookupRef(ref RoleRef) (Role, bool) {
	r, ok := c.roles[ref.Name]
	if !ok || originOf(r.Layer) != ref.Origin {
		return Role{}, false
	}
	return r, true
}

// originOf memetakan Layer definisi ke lapis klaim tempat nama itu semestinya datang.
func originOf(l Layer) RoleOrigin {
	if l == LayerTenant {
		return OriginTenant
	}
	return OriginCentral
}
