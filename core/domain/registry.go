package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Registry adalah sumber kebenaran tunggal modul yang terdaftar (CLAUDE.md #22).
// Modul didaftarkan eksplisit lewat Register; tidak ada penemuan implisit.
// Validate() menegakkan invariant lintas-modul saat boot (fail-fast, philosophy #4).
type Registry struct {
	byName map[string]Module
	order  []string // urutan registrasi, untuk listing deterministik
}

// NewRegistry membuat registry kosong.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Module)}
}

// Register menambahkan modul ke registry. Duplikasi nama & nama kosong tidak ditolak
// di sini melainkan saat Validate, agar seluruh pelanggaran bisa dilaporkan sekaligus.
// order memuat SETIAP kemunculan (termasuk duplikat) untuk deteksi di Validate;
// listing dideduplikasi belakangan.
func (r *Registry) Register(modules ...Module) {
	for _, m := range modules {
		name := m.Manifest().Name
		r.order = append(r.order, name)
		r.byName[name] = m
	}
}

// Modules mengembalikan modul unik dalam urutan registrasi pertama.
func (r *Registry) Modules() []Module {
	seen := make(map[string]bool)
	out := make([]Module, 0, len(r.byName))
	for _, name := range r.order {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, r.byName[name])
	}
	return out
}

// Get mengembalikan modul berdasarkan nama.
func (r *Registry) Get(name string) (Module, bool) {
	m, ok := r.byName[name]
	return m, ok
}

// Validate memeriksa invariant lintas-modul. Mengembalikan error gabungan agar caller
// (boot) bisa gagal cepat dengan pesan lengkap. Yang diperiksa:
//   - nama modul tidak boleh kosong
//   - nama modul tidak boleh duplikat
//   - DependsOn harus menunjuk modul terdaftar (tidak boleh menggantung)
//   - graf DependsOn harus DAG (tidak boleh ada siklus)
func (r *Registry) Validate() error {
	var errs []string

	// Hitung kemunculan tiap nama untuk deteksi duplikat (order memuat kemunculan kedua).
	count := make(map[string]int)
	for _, name := range r.order {
		count[name]++
	}
	for name, n := range count {
		if name == "" {
			errs = append(errs, "ada modul tanpa Name (Manifest.Name kosong)")
		}
		if n > 1 {
			errs = append(errs, fmt.Sprintf("nama modul duplikat: %q terdaftar %d kali", name, n))
		}
	}

	// Dependency menggantung: DependsOn ke modul tak terdaftar.
	for _, name := range r.uniqueNames() {
		for _, dep := range r.byName[name].Manifest().DependsOn {
			if _, ok := r.byName[dep]; !ok {
				errs = append(errs, fmt.Sprintf("modul %q bergantung pada %q yang tidak terdaftar", name, dep))
			}
		}
	}

	// Deteksi siklus DAG (hanya bila tidak ada dependency menggantung yang sudah dilaporkan).
	if cycle := r.findCycle(); cycle != nil {
		errs = append(errs, fmt.Sprintf("dependency sirkular terdeteksi: %s", strings.Join(cycle, " -> ")))
	}

	// Validasi tiap entity + keunikan nama tabel lintas-modul (PRD F2).
	tableOwner := make(map[string]string) // tablename -> "modul.Entity"
	for _, name := range r.uniqueNames() {
		for _, ent := range r.byName[name].Manifest().Entities {
			if err := ent.Validate(); err != nil {
				errs = append(errs, fmt.Sprintf("modul %q: %s", name, err.Error()))
			}
			ref := name + "." + ent.Name
			table := ent.TableName()
			if prev, dup := tableOwner[table]; dup {
				errs = append(errs, fmt.Sprintf("tabel %q diklaim dua entity: %s dan %s",
					table, prev, ref))
			} else {
				tableOwner[table] = ref
			}
		}
	}

	// Validasi export/import permission antar modul (PR-2.3.4).
	errs = append(errs, r.validatePermissions()...)

	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("registry modul tidak valid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// validatePermissions menegakkan kontrak export/import permission antar modul
// (CLAUDE.md "Konvensi penamaan → Permission", PR-2.3.4):
//   - setiap Exports harus permission yang didefinisikan modul itu sendiri di Groups,
//     dan ber-prefix nama modul ({modul}:{entity}:{aksi});
//   - setiap Imports.From harus modul terdaftar yang benar-benar meng-export
//     permission tersebut, dan Permission ber-prefix From.
//
// Ini pasangan boot-time (cross-manifest) dari linter permission-must-be-registered
// yang menegakkan sisi penggunaan kode. Validasi di sini menutup loop: deklarasi
// import yang menggantung atau tak ter-export gagal cepat saat boot.
func (r *Registry) validatePermissions() []string {
	var errs []string

	// Peta permission yang di-export tiap modul + permission yang didefinisikan di Groups.
	exportsByModule := make(map[string]map[string]bool)
	definedByModule := make(map[string]map[string]bool)
	for _, name := range r.uniqueNames() {
		pm := r.byName[name].Manifest().Permissions
		defined := make(map[string]bool)
		for _, g := range pm.Groups {
			for _, p := range g.Permissions {
				defined[p.Name] = true
			}
		}
		definedByModule[name] = defined
		exp := make(map[string]bool)
		for _, e := range pm.Exports {
			exp[e] = true
		}
		exportsByModule[name] = exp
	}

	for _, name := range r.uniqueNames() {
		pm := r.byName[name].Manifest().Permissions

		for _, e := range pm.Exports {
			if permModule(e) != name {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-export permission %q yang bukan miliknya (prefix harus %q)", name, e, name))
				continue
			}
			if !definedByModule[name][e] {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-export permission %q yang tidak didefinisikan di Groups-nya", name, e))
			}
		}

		for _, imp := range pm.Imports {
			if permModule(imp.Permission) != imp.From {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-import %q dari %q tetapi prefix permission bukan %q",
					name, imp.Permission, imp.From, imp.From))
				continue
			}
			if imp.From == name {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-import permission dari dirinya sendiri (%q) — pakai langsung tanpa Imports",
					name, imp.Permission))
				continue
			}
			if _, ok := r.byName[imp.From]; !ok {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-import dari modul %q yang tidak terdaftar", name, imp.From))
				continue
			}
			if !exportsByModule[imp.From][imp.Permission] {
				errs = append(errs, fmt.Sprintf(
					"modul %q meng-import %q dari %q tetapi modul itu tidak meng-export-nya",
					name, imp.Permission, imp.From))
			}
		}
	}
	return errs
}

// ExportedPermissions mengembalikan semua permission yang di-export modul terdaftar,
// dipetakan permission -> nama modul pemilik. Ini katalog permission lintas-modul yang
// dikenal sistem; dipakai validasi import dan dapat dirujuk konsumen lain (mis. saat
// wiring auth) tanpa menyentuh paket modul.
func (r *Registry) ExportedPermissions() map[string]string {
	out := make(map[string]string)
	for _, name := range r.uniqueNames() {
		for _, e := range r.byName[name].Manifest().Permissions.Exports {
			out[e] = name
		}
	}
	return out
}

// permModule mengembalikan segmen modul (sebelum titik dua pertama) dari sebuah
// string permission {modul}:{entity}:{aksi}. Mengembalikan "" bila tak ada titik dua.
func permModule(perm string) string {
	if i := strings.Index(perm, ":"); i >= 0 {
		return perm[:i]
	}
	return ""
}

// uniqueNames mengembalikan nama modul unik (urutan registrasi).
func (r *Registry) uniqueNames() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(r.byName))
	for _, name := range r.order {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// findCycle mengembalikan jejak siklus pertama yang ditemukan (mis. ["a","b","a"]),
// atau nil bila graf DependsOn adalah DAG. Dependency ke modul tak terdaftar diabaikan
// di sini (sudah dilaporkan terpisah) agar pesan tidak rancu.
func (r *Registry) findCycle() []string {
	const (
		white = 0 // belum dikunjungi
		gray  = 1 // sedang di stack (jalur aktif)
		black = 2 // selesai
	)
	color := make(map[string]int)
	var stack []string

	var visit func(name string) []string
	visit = func(name string) []string {
		color[name] = gray
		stack = append(stack, name)
		for _, dep := range r.byName[name].Manifest().DependsOn {
			if _, ok := r.byName[dep]; !ok {
				continue // dependency menggantung, dilewati
			}
			switch color[dep] {
			case gray:
				// Temukan awal siklus di stack lalu kembalikan jejaknya.
				for i, n := range stack {
					if n == dep {
						return append(append([]string{}, stack[i:]...), dep)
					}
				}
			case white:
				if c := visit(dep); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[name] = black
		return nil
	}

	for _, name := range r.uniqueNames() {
		if color[name] == white {
			if c := visit(name); c != nil {
				return c
			}
		}
	}
	return nil
}
