package scheduler

import "sort"

// Registry memetakan JobKey → JobFunc. Pola registry seragam framework (titik ekstensi #1):
// menambah job baru = tulis handler + daftarkan satu baris, kode pemanggil tak berubah.
// Diisi saat bootstrap lalu dianggap immutable; Register menolak key ganda/handler nil.
type Registry struct {
	jobs map[string]JobFunc
}

// NewRegistry membuat registry kosong.
func NewRegistry() *Registry {
	return &Registry{jobs: make(map[string]JobFunc)}
}

// Register mendaftarkan handler untuk key. Menolak key ganda (ErrJobKeyExists) dan
// handler nil (ErrNilJobFunc) — keduanya menandakan salah wiring saat bootstrap.
func (r *Registry) Register(key string, fn JobFunc) error {
	if fn == nil {
		return ErrNilJobFunc(key)
	}
	if _, exists := r.jobs[key]; exists {
		return ErrJobKeyExists(key)
	}
	r.jobs[key] = fn
	return nil
}

// Get mengembalikan handler untuk key, atau ErrJobKeyNotRegistered bila tak ada.
func (r *Registry) Get(key string) (JobFunc, error) {
	fn, ok := r.jobs[key]
	if !ok {
		return nil, ErrJobKeyNotRegistered(key)
	}
	return fn, nil
}

// Keys mengembalikan seluruh key terdaftar terurut — untuk introspeksi/validasi jadwal.
func (r *Registry) Keys() []string {
	keys := make([]string, 0, len(r.jobs))
	for k := range r.jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
