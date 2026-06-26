package permission

// RoleCatalog memetakan nama role ke definisinya. PR-2.3.1 menyediakan implementasi
// in-memory (MemoryCatalog). PR-2.3.2/2.3.3 menambah implementasi berbasis DB
// (central di id.*, tenant di gov.*) tanpa mengubah Engine — penerapan
// titik ekstensi #1 (registry pattern, Open/Closed) pada CLAUDE.md.
type RoleCatalog interface {
	// Lookup mengembalikan definisi role dan true bila terdaftar.
	Lookup(name string) (Role, bool)
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
