package db

import (
	"context"
	"fmt"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// Enkripsi field selektif di lapis repository (ADR-009 §4). Dipanggil OTOMATIS oleh
// SQLRepository berdasarkan klasifikasi field — bukan oleh use case, karena developer modul
// pasti lupa. Satu field logis terenkripsi = dua kolom fisik ({f}_enc, {f}_bidx); plaintext-nya
// tak pernah disimpan.
//
// Repo yang dibangun TANPA spec kripto berperilaku persis seperti sebelumnya (jalur cepat
// nil) — enkripsi hanya aktif untuk entity yang memang mendeklarasikannya.

// FieldCryptoSpec memetakan satu kolom logis ke perlakuan kriptonya.
//
// Purpose memisahkan konteks kunci (ADR-010 §2) dan default-nya = nama kolom, sehingga
// `nik` dan `no_rekening` memakai DEK berbeda tanpa konfigurasi tambahan. Searchable
// menentukan ada tidaknya kolom _bidx: tanpa itu nilai hanya bisa dibaca, tak bisa dicari
// maupun di-UNIQUE-kan.
type FieldCryptoSpec struct {
	Column     string
	Purpose    string
	Searchable bool
}

// FieldCryptoFromEntity menurunkan spec dari EntityDef — jalur yang DIANJURKAN. Menurunkan,
// bukan menulis tangan, menutup celah paling berbahaya di skema ini: spec yang menyimpang
// dari deklarasi field membuat kolom yang seharusnya terenkripsi tersimpan plaintext tanpa
// ada yang menyadarinya.
func FieldCryptoFromEntity(e domain.EntityDef) []FieldCryptoSpec {
	var out []FieldCryptoSpec
	for _, f := range e.Fields {
		if !f.Class.IsEncrypted() {
			continue
		}
		out = append(out, FieldCryptoSpec{
			Column:     f.Name,
			Purpose:    f.Name,
			Searchable: f.Searchable,
		})
	}
	return out
}

// fieldCrypto menerjemahkan antara kolom logis (yang dikenal Mapper) dan kolom fisik
// (yang ada di tabel). Ia memegang urutan kolom logis agar pemetaan posisi saat Scan
// tidak bergantung pada asumsi pemanggil.
type fieldCrypto struct {
	crypto port.CryptoPort
	// byColumn dan order dijaga konsisten: order = DataColumns() apa adanya.
	byColumn map[string]FieldCryptoSpec
	order    []string
}

func newFieldCrypto(c port.CryptoPort, dataColumns []string, specs []FieldCryptoSpec) (*fieldCrypto, error) {
	if c == nil {
		return nil, fmt.Errorf("field crypto: CryptoPort wajib")
	}
	known := make(map[string]bool, len(dataColumns))
	for _, c := range dataColumns {
		known[c] = true
	}
	byColumn := make(map[string]FieldCryptoSpec, len(specs))
	for _, s := range specs {
		if !known[s.Column] {
			// Salah ketik nama kolom akan membuat field tersimpan PLAINTEXT tanpa gejala —
			// karena itu ditolak saat konstruksi, bukan dibiarkan.
			return nil, fmt.Errorf("field crypto: kolom %q tidak ada di DataColumns mapper", s.Column)
		}
		if s.Purpose == "" {
			s.Purpose = s.Column
		}
		if _, dup := byColumn[s.Column]; dup {
			return nil, fmt.Errorf("field crypto: kolom %q dideklarasikan dua kali", s.Column)
		}
		byColumn[s.Column] = s
	}
	return &fieldCrypto{crypto: c, byColumn: byColumn, order: dataColumns}, nil
}

func (f *fieldCrypto) spec(column string) (FieldCryptoSpec, bool) {
	s, ok := f.byColumn[column]
	return s, ok
}

// writeColumns mengembalikan kolom FISIK untuk INSERT/UPDATE: kolom biasa apa adanya,
// kolom terenkripsi menjadi _enc (+ _bidx bila searchable).
func (f *fieldCrypto) writeColumns() []string {
	out := make([]string, 0, len(f.order))
	for _, c := range f.order {
		s, ok := f.spec(c)
		if !ok {
			out = append(out, c)
			continue
		}
		out = append(out, encColumn(c))
		if s.Searchable {
			out = append(out, bidxColumn(c))
		}
	}
	return out
}

// readColumns mengembalikan kolom FISIK untuk SELECT. _bidx TIDAK ikut dibaca: ia hanya
// alat pencarian, bukan sumber nilai.
func (f *fieldCrypto) readColumns() []string {
	out := make([]string, 0, len(f.order))
	for _, c := range f.order {
		if _, ok := f.spec(c); ok {
			out = append(out, encColumn(c))
			continue
		}
		out = append(out, c)
	}
	return out
}

// writeValues menyejajarkan nilai dengan writeColumns: nilai kolom terenkripsi diganti
// ciphertext (+ blind index). Nilai KOSONG disimpan NULL di kedua kolom — bukan ciphertext
// dari string kosong: blind index atas nilai kosong akan menandai baris mana yang kosong,
// dan UNIQUE-nya akan menolak baris kosong kedua.
func (f *fieldCrypto) writeValues(ctx context.Context, values []any) ([]any, error) {
	if len(values) != len(f.order) {
		return nil, fmt.Errorf("field crypto: jumlah nilai (%d) tak sama dengan kolom (%d)", len(values), len(f.order))
	}
	tenantID, err := f.tenant(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, len(values)+len(f.byColumn))
	for i, col := range f.order {
		s, ok := f.spec(col)
		if !ok {
			out = append(out, values[i])
			continue
		}
		plain, isNull, err := plaintextOf(col, values[i])
		if err != nil {
			return nil, err
		}
		if isNull {
			out = append(out, nil)
			if s.Searchable {
				out = append(out, nil)
			}
			continue
		}
		ct, err := f.crypto.Encrypt(ctx, tenantID, s.Purpose, plain)
		if err != nil {
			return nil, fmt.Errorf("enkripsi kolom %s: %w", col, err)
		}
		out = append(out, ct)
		if s.Searchable {
			bidx, err := f.crypto.BlindIndex(ctx, tenantID, s.Purpose, plain)
			if err != nil {
				return nil, fmt.Errorf("blind index kolom %s: %w", col, err)
			}
			out = append(out, bidx)
		}
	}
	return out, nil
}

// filterExpr menerjemahkan filter equality atas kolom terenkripsi menjadi perbandingan di
// kolom _bidx. Kolom terenkripsi TANPA blind index tak bisa difilter sama sekali — dan itu
// dilaporkan sebagai error, bukan diam-diam mengembalikan hasil kosong (kegagalan senyap
// pada filter keamanan lebih berbahaya daripada error).
func (f *fieldCrypto) filterExpr(ctx context.Context, column string, value any) (expr string, arg any, err error) {
	s, ok := f.spec(column)
	if !ok {
		return "", nil, nil // bukan kolom terenkripsi — penanganan biasa
	}
	if !s.Searchable {
		return "", nil, fmt.Errorf(
			"kolom %s terenkripsi tanpa blind index sehingga tak bisa difilter (ubah Searchable di EntityDef bila perlu dicari)", column)
	}
	tenantID, err := f.tenant(ctx)
	if err != nil {
		return "", nil, err
	}
	plain, isNull, err := plaintextOf(column, value)
	if err != nil {
		return "", nil, err
	}
	if isNull {
		return bidxColumn(column) + " IS NULL", nil, nil
	}
	bidx, err := f.crypto.BlindIndex(ctx, tenantID, s.Purpose, plain)
	if err != nil {
		return "", nil, fmt.Errorf("blind index filter %s: %w", column, err)
	}
	return bidxColumn(column) + " = ?", bidx, nil
}

// tenant mengambil tenant aktif dari context. Tenant kosong = GAGAL, tidak pernah memakai
// nilai default: hierarki kunci per-tenant kehilangan artinya bila tenant bisa kosong.
func (f *fieldCrypto) tenant(ctx context.Context) (string, error) {
	tenantID := port.TenantFrom(ctx)
	if tenantID == "" {
		return "", fmt.Errorf("field crypto: tenant tidak diketahui dari context (kunci enkripsi per-tenant)")
	}
	return tenantID, nil
}

// plaintextOf menormalkan nilai dari Mapper.DataValues menjadi byte untuk dienkripsi.
// Hanya tipe teks yang didukung: field terenkripsi memang dibatasi ke Text oleh
// FieldDef.Validate (ADR-009 §5), jadi tipe lain di sini berarti spec dan EntityDef
// menyimpang — kondisi yang harus berisik, bukan dikonversi diam-diam.
func plaintextOf(column string, v any) (plain []byte, isNull bool, err error) {
	switch val := v.(type) {
	case nil:
		return nil, true, nil
	case string:
		if val == "" {
			return nil, true, nil
		}
		return []byte(val), false, nil
	case *string:
		if val == nil || *val == "" {
			return nil, true, nil
		}
		return []byte(*val), false, nil
	case []byte:
		if len(val) == 0 {
			return nil, true, nil
		}
		return val, false, nil
	default:
		return nil, false, fmt.Errorf(
			"kolom %s terenkripsi tapi nilainya bertipe %T (hanya teks yang didukung)", column, v)
	}
}

// decryptingScanner menyisip di antara baris DB dan Mapper.Scan. Mapper menyerahkan pointer
// tujuan seperti biasa; scanner ini menukar pointer di posisi kolom terenkripsi dengan
// penampung []byte, membaca ciphertext, mendekripsinya, lalu menulis hasilnya ke pointer asli.
// Dengan begitu Mapper tetap polos — ia tak perlu tahu ada kripto sama sekali.
type decryptingScanner struct {
	row     port.Row
	fc      *fieldCrypto
	ctx     context.Context
	columns []string // kolom logis, sejajar posisi dest[1..len]
}

// Scan mengharapkan urutan dest: id, data..., version — kontrak yang sama dengan selectCols.
func (s *decryptingScanner) Scan(dest ...any) error {
	want := len(s.columns) + 2 // id + data + version
	if len(dest) != want {
		return fmt.Errorf("field crypto: Mapper.Scan meminta %d kolom, tabel menyediakan %d", len(dest), want)
	}
	tenantID, err := s.fc.tenant(s.ctx)
	if err != nil {
		return err
	}

	holders := make(map[int]*[]byte, len(s.fc.byColumn))
	scanDest := make([]any, len(dest))
	copy(scanDest, dest)
	for i, col := range s.columns {
		if _, ok := s.fc.spec(col); !ok {
			continue
		}
		var raw []byte
		holders[i+1] = &raw
		scanDest[i+1] = &raw
	}

	if err := s.row.Scan(scanDest...); err != nil {
		return err
	}

	for pos, raw := range holders {
		col := s.columns[pos-1]
		spec, _ := s.fc.spec(col)
		if len(*raw) == 0 { // NULL / kosong
			continue
		}
		// Pengikatan KOLOM: AAD ciphertext hanya mengikat tenant, sehingga blob yang dipindah
		// antar kolom dalam satu tenant tetap terbuka. Pemeriksaan purpose inilah penegaknya
		// (lihat crypto.PurposeOf) — tanpa ini, nilai `nik` yang disalin ke `no_rekening_enc`
		// akan terbaca sebagai no_rekening yang sah.
		gotPurpose, err := s.fc.crypto.PurposeOf(*raw)
		if err != nil {
			return fmt.Errorf("kolom %s: %w", col, err)
		}
		if gotPurpose != spec.Purpose {
			return fmt.Errorf(
				"kolom %s memuat ciphertext ber-purpose %q, bukan %q — data dipindah antar kolom",
				col, gotPurpose, spec.Purpose)
		}
		plain, err := s.fc.crypto.Decrypt(s.ctx, tenantID, *raw)
		if err != nil {
			return fmt.Errorf("dekripsi kolom %s: %w", col, err)
		}
		if err := assignPlaintext(dest[pos], plain); err != nil {
			return fmt.Errorf("kolom %s: %w", col, err)
		}
	}
	return nil
}

// assignPlaintext menulis hasil dekripsi ke pointer tujuan milik Mapper.
func assignPlaintext(dest any, plain []byte) error {
	switch d := dest.(type) {
	case *string:
		*d = string(plain)
		return nil
	case *[]byte:
		*d = plain
		return nil
	case **string:
		s := string(plain)
		*d = &s
		return nil
	default:
		return fmt.Errorf("tujuan scan bertipe %T tidak didukung untuk field terenkripsi", dest)
	}
}
