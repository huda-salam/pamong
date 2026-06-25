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
			if prev, dup := tableOwner[ent.Tablename]; dup {
				errs = append(errs, fmt.Sprintf("tabel %q diklaim dua entity: %s dan %s",
					ent.Tablename, prev, ref))
			} else {
				tableOwner[ent.Tablename] = ref
			}
		}
	}

	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("registry modul tidak valid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
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
