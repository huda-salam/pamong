package strategy

import "sort"

// CoherenceValidator memeriksa apakah SATU kombinasi pilihan tenant lintas decision point
// koheren. `choices` memetakan decision point → key terpilih (mis.
// {"keuangan.persediaan": "keuangan.persediaan.fifo", "aset.pendekatan": "aset.pendekatan.beban"}).
// Mengembalikan error bila kombinasi tak masuk akal secara akuntansi/kebijakan; nil bila sah.
//
// Validator adalah kode Go ter-test (bukan aturan tersimpan di DB) — konsisten dengan prinsip
// strategy: yang di DB hanya pilihan (key), koherensinya ditegakkan kode.
type CoherenceValidator func(choices map[string]string) error

// CoherenceRegistry adalah titik ekstensi #5 (CLAUDE.md): tempat mendaftarkan validator
// kombinasi lintas-pilihan. Disiapkan meski belum tentu dipakai di awal — menambah aturan
// koherensi nanti = daftarkan validator baru, tanpa mengubah alur pemilihan (open/closed).
//
// Dipakai use case admin saat tenant mengubah pilihan: kumpulkan seluruh pilihan tenant lalu
// Validate sebelum commit; kombinasi tak koheren → tolak (PRD F5).
type CoherenceRegistry struct {
	validators map[string]CoherenceValidator
}

// NewCoherenceRegistry membuat registry kosong.
func NewCoherenceRegistry() *CoherenceRegistry {
	return &CoherenceRegistry{validators: make(map[string]CoherenceValidator)}
}

// Register mendaftarkan satu validator di bawah nama unik (untuk identifikasi saat gagal).
// Nama ganda → ErrCoherenceValidatorExists (bug wiring); validator nil → ErrInvalidKey.
func (c *CoherenceRegistry) Register(name string, v CoherenceValidator) error {
	if name == "" {
		return ErrInvalidKey(name, "nama coherence validator kosong")
	}
	if v == nil {
		return ErrInvalidKey(name, "coherence validator tidak boleh nil")
	}
	if _, dup := c.validators[name]; dup {
		return ErrCoherenceValidatorExists(name)
	}
	c.validators[name] = v
	return nil
}

// Validate menjalankan SELURUH validator terdaftar terhadap kombinasi pilihan, dalam urutan
// nama yang deterministik. Gagal pada validator pertama yang menolak; error dibungkus dengan
// nama validator agar jelas aturan mana yang dilanggar. Tanpa validator → selalu koheren.
func (c *CoherenceRegistry) Validate(choices map[string]string) error {
	names := make([]string, 0, len(c.validators))
	for name := range c.validators {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := c.validators[name](choices); err != nil {
			return ErrIncoherentCombination(name, err.Error())
		}
	}
	return nil
}
