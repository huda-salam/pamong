package db

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// RowScanner adalah abstraksi minimal untuk men-scan satu baris ke field tujuan.
// Baik port.Row maupun port.Rows memenuhinya, sehingga Mapper.Scan bisa dipakai
// untuk hasil tunggal (FindByID) maupun iterasi (List).
type RowScanner interface {
	Scan(dest ...any) error
}

// Mapper menjembatani entity domain T dengan representasi tabelnya. Domain tidak
// pernah mengimplementasi Mapper — implementasinya hidup di adapter/db tiap modul,
// sehingga domain tetap bebas dependensi infrastruktur (hexagonal).
//
// Kontrak kolom: SQLRepository selalu memilih kolom dengan urutan
// id, <DataColumns...>, version. Implementasi Scan WAJIB men-scan dengan urutan
// yang sama. Framework mengelola sendiri kolom version, created_at, updated_at,
// dan deleted_at — Mapper hanya mendeklarasikan kolom bisnis lewat DataColumns.
//
// Tujuan scan kolom id WAJIB terbaca kembali sebagai UUID (*uuid.UUID, *string, atau
// *[]byte berisi UUID). Ini bukan sekadar kerapian: untuk entity ber-field terenkripsi,
// id baris itulah yang mengikat ciphertext ke barisnya (ADR-016), dan framework
// membacanya dari tujuan scan pertama. Tipe lain akan menggagalkan setiap pembacaan
// entity tersebut — bukan hanya kolom terenkripsinya.
type Mapper[T any] interface {
	Table() string                 // nama tabel lengkap "schema.tabel"
	DataColumns() []string         // kolom bisnis, tanpa id/version/timestamp
	DataValues(e *T) []any         // nilai sejajar DataColumns (urutan sama)
	Scan(s RowScanner) (*T, error) // scan urutan: id, data..., version
	ID(e *T) uuid.UUID
	Version(e *T) int
	SetVersion(e *T, v int)
	SearchColumns() []string // kolom untuk pencarian ILIKE; nil jika tak didukung
}

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
var tableRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*)?$`)

// SQLRepository adalah implementasi generik port.BaseRepository di atas port.DBConn.
// Ia menangani idempotensi kolom standar framework: optimistic locking lewat
// version, soft delete lewat deleted_at, serta pagination/sort/filter untuk List.
type SQLRepository[T any] struct {
	conn        port.DBConn
	m           Mapper[T]
	selectCols  string          // "id, data1, data2, version"
	allowedCols map[string]bool // kolom yang boleh dipakai untuk sort/filter
	fc          *fieldCrypto    // nil = tak ada field terenkripsi (jalur lama, tanpa perubahan)
}

// repoOptions menampung konfigurasi opsional repo generik.
type repoOptions struct {
	crypto      port.CryptoPort
	cryptoSpecs []FieldCryptoSpec
}

// RepoOption menyesuaikan perilaku SQLRepository saat konstruksi.
type RepoOption func(*repoOptions)

// WithFieldCrypto mengaktifkan enkripsi field selektif (ADR-009) untuk kolom di specs.
// Specs SEBAIKNYA diturunkan dari EntityDef lewat FieldCryptoFromEntity, bukan ditulis
// tangan — spec yang menyimpang dari deklarasi field membuat kolom yang seharusnya
// terenkripsi tersimpan plaintext tanpa gejala apa pun.
func WithFieldCrypto(c port.CryptoPort, specs []FieldCryptoSpec) RepoOption {
	return func(o *repoOptions) {
		o.crypto = c
		o.cryptoSpecs = specs
	}
}

// NewSQLRepository membuat repository generik. Identifier tabel & kolom divalidasi
// di sini agar tidak ada celah injeksi lewat nama kolom dinamis (sort/filter).
func NewSQLRepository[T any](conn port.DBConn, m Mapper[T], opts ...RepoOption) (*SQLRepository[T], error) {
	if !tableRe.MatchString(m.Table()) {
		return nil, fmt.Errorf("nama tabel tidak valid: %q", m.Table())
	}
	var o repoOptions
	for _, opt := range opts {
		opt(&o)
	}
	allowed := map[string]bool{"id": true, "version": true, "created_at": true, "updated_at": true}
	for _, c := range m.DataColumns() {
		if !identRe.MatchString(c) {
			return nil, fmt.Errorf("nama kolom tidak valid: %q", c)
		}
		allowed[c] = true
	}
	for _, c := range m.SearchColumns() {
		if !allowed[c] {
			return nil, fmt.Errorf("search column %q bukan kolom yang dideklarasikan", c)
		}
	}

	var fc *fieldCrypto
	if len(o.cryptoSpecs) > 0 {
		var err error
		if fc, err = newFieldCrypto(o.crypto, m.DataColumns(), o.cryptoSpecs); err != nil {
			return nil, err
		}
		// Pencarian ILIKE mustahil di atas ciphertext (nonce acak). Ditolak saat konstruksi
		// agar tak menjadi hasil pencarian yang diam-diam selalu kosong.
		for _, c := range m.SearchColumns() {
			if _, encrypted := fc.spec(c); encrypted {
				return nil, fmt.Errorf(
					"kolom %q terenkripsi sehingga tak bisa jadi search column (ILIKE tak mungkin di atas ciphertext)", c)
			}
		}
	}

	dataCols := m.DataColumns()
	if fc != nil {
		dataCols = fc.readColumns()
	}
	cols := append([]string{"id"}, dataCols...)
	cols = append(cols, "version")
	return &SQLRepository[T]{
		conn:        conn,
		m:           m,
		selectCols:  strings.Join(cols, ", "),
		allowedCols: allowed,
		fc:          fc,
	}, nil
}

// scanner membungkus baris agar kolom terenkripsi didekripsi sebelum sampai ke Mapper.
// Tanpa field crypto ia mengembalikan baris apa adanya (nol overhead).
func (r *SQLRepository[T]) scanner(ctx context.Context, row port.Row) port.Row {
	if r.fc == nil {
		return row
	}
	return &decryptingScanner{row: row, fc: r.fc, ctx: ctx, columns: r.m.DataColumns()}
}

// writeCols & writeVals memberi kolom + nilai FISIK untuk INSERT/UPDATE.
func (r *SQLRepository[T]) writeCols() []string {
	if r.fc == nil {
		return r.m.DataColumns()
	}
	return r.fc.writeColumns()
}

func (r *SQLRepository[T]) writeVals(ctx context.Context, entity *T) ([]any, error) {
	if r.fc == nil {
		return r.m.DataValues(entity), nil
	}
	return r.fc.writeValues(ctx, r.m.ID(entity), r.m.DataValues(entity))
}

var _ port.BaseRepository[struct{}] = (*SQLRepository[struct{}])(nil)

func (r *SQLRepository[T]) FindByID(ctx context.Context, id uuid.UUID) (*T, error) {
	sql := fmt.Sprintf(
		"SELECT %s FROM %s WHERE id = $1 AND deleted_at IS NULL",
		r.selectCols, r.m.Table(),
	)
	entity, err := r.m.Scan(r.scanner(ctx, r.conn.QueryRow(ctx, sql, id)))
	if IsNoRows(err) {
		return nil, core.ErrNotFound(r.m.Table(), id.String())
	}
	if err != nil {
		return nil, err
	}
	return entity, nil
}

// Save menyisipkan entity baru dengan version = 1.
func (r *SQLRepository[T]) Save(ctx context.Context, entity *T) error {
	data := r.writeCols()
	cols := append([]string{"id"}, data...)
	cols = append(cols, "version", "created_at", "updated_at")

	ph := make([]string, 0, len(data)+1)
	for i := range data {
		ph = append(ph, fmt.Sprintf("$%d", i+2)) // $1 dipakai id
	}
	vals, err := r.writeVals(ctx, entity)
	if err != nil {
		return err
	}
	values := append([]any{r.m.ID(entity)}, vals...)

	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES ($1, %s, 1, now(), now())",
		r.m.Table(), strings.Join(cols, ", "), strings.Join(ph, ", "),
	)
	if _, err := r.conn.Exec(ctx, sql, values...); err != nil {
		return err
	}
	r.m.SetVersion(entity, 1)
	return nil
}

// Update menerapkan optimistic locking: hanya berhasil jika version di DB sama
// dengan version entity. Konflik (version bergeser / baris terhapus) -> ErrConflict.
func (r *SQLRepository[T]) Update(ctx context.Context, entity *T) error {
	data := r.writeCols()
	sets := make([]string, 0, len(data))
	for i, c := range data {
		sets = append(sets, fmt.Sprintf("%s = $%d", c, i+1))
	}
	idPos := len(data) + 1
	verPos := len(data) + 2

	sql := fmt.Sprintf(
		"UPDATE %s SET %s, version = version + 1, updated_at = now() "+
			"WHERE id = $%d AND version = $%d AND deleted_at IS NULL RETURNING version",
		r.m.Table(), strings.Join(sets, ", "), idPos, verPos,
	)
	vals, err := r.writeVals(ctx, entity)
	if err != nil {
		return err
	}
	args := append(vals, r.m.ID(entity), r.m.Version(entity))

	var newVersion int
	err = r.conn.QueryRow(ctx, sql, args...).Scan(&newVersion)
	if IsNoRows(err) {
		return core.ErrConflict(fmt.Sprintf(
			"%s id=%s telah diubah pihak lain atau tidak ada (optimistic lock)",
			r.m.Table(), r.m.ID(entity),
		))
	}
	if err != nil {
		return err
	}
	r.m.SetVersion(entity, newVersion)
	return nil
}

// SoftDelete menandai baris terhapus tanpa menghapus fisik (deleted_at = now()).
func (r *SQLRepository[T]) SoftDelete(ctx context.Context, id uuid.UUID) error {
	sql := fmt.Sprintf(
		"UPDATE %s SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL",
		r.m.Table(),
	)
	tag, err := r.conn.Exec(ctx, sql, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound(r.m.Table(), id.String())
	}
	return nil
}

func (r *SQLRepository[T]) List(ctx context.Context, filter port.ListFilter) (*port.ListResult[T], error) {
	where := []string{"deleted_at IS NULL"}
	args := []any{}

	// Filter exact-match — hanya kolom yang dideklarasikan yang diterima.
	for col, val := range filter.Filters {
		if !r.allowedCols[col] {
			return nil, core.ErrValidation("filter", fmt.Sprintf("kolom filter tidak dikenal: %s", col))
		}
		// Kolom terenkripsi tak bisa dibandingkan langsung (nonce acak) — equality-nya
		// dialihkan ke blind index (ADR-009 §2). Kolom biasa lewat jalur lama.
		if r.fc != nil {
			expr, arg, err := r.fc.filterExpr(ctx, col, val)
			if err != nil {
				return nil, core.ErrValidation("filter", err.Error())
			}
			if expr != "" {
				if arg == nil { // "... IS NULL", tanpa argumen
					where = append(where, expr)
					continue
				}
				args = append(args, arg)
				where = append(where, strings.Replace(expr, "?", fmt.Sprintf("$%d", len(args)), 1))
				continue
			}
		}
		args = append(args, val)
		where = append(where, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	// Pencarian ILIKE lintas kolom search.
	if s := strings.TrimSpace(filter.Search); s != "" {
		sc := r.m.SearchColumns()
		if len(sc) == 0 {
			return nil, core.ErrValidation("search", "entity ini tidak mendukung pencarian teks")
		}
		args = append(args, "%"+s+"%")
		ors := make([]string, len(sc))
		for i, c := range sc {
			ors[i] = fmt.Sprintf("%s ILIKE $%d", c, len(args))
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	whereSQL := strings.Join(where, " AND ")
	table := r.m.Table()

	var total int64
	countSQL := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", table, whereSQL)
	if err := r.conn.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, pageSize := normalizePaging(filter.Page, filter.PageSize)
	orderBy, err := r.orderClause(filter.Sort, filter.Order)
	if err != nil {
		return nil, err
	}

	listSQL := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		r.selectCols, table, whereSQL, orderBy, len(args)+1, len(args)+2,
	)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := r.conn.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]T, 0, pageSize)
	for rows.Next() {
		// Satu baris yang gagal didekripsi menggagalkan SELURUH halaman, dan itu disengaja.
		// Dua alternatif yang lebih ramah justru berbahaya di jalur data: melewati baris
		// membuat baris yang ciphertext-nya dirusak MENGHILANG dari daftar (perusakan jadi
		// alat penyembunyian), sedangkan mengosongkan field membuat nilai yang tak terbaca
		// tampak seperti nilai yang memang kosong. Repository adalah sumber kebenaran, bukan
		// laporan — kegagalan di sini harus terlihat. Jalur baca audit (core/audit.Reader)
		// boleh mendegradasi anggun karena ia memang menampilkan bukti, bukan menyuplai data
		// untuk keputusan. Error dari scanner menyebut id baris agar tetap bisa ditindak.
		e, err := r.m.Scan(r.scanner(ctx, rows))
		if err != nil {
			return nil, err
		}
		items = append(items, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return &port.ListResult[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// orderClause memvalidasi kolom sort terhadap whitelist & arah asc/desc.
func (r *SQLRepository[T]) orderClause(sort, order string) (string, error) {
	if sort == "" {
		sort = "created_at"
	}
	if !r.allowedCols[sort] {
		return "", core.ErrValidation("sort", fmt.Sprintf("kolom sort tidak dikenal: %s", sort))
	}
	// Urutan atas ciphertext tidak bermakna (nonce acak) dan blind index hanya menopang
	// equality. Ditolak lantang — mengurutkan kolom _enc akan menghasilkan urutan acak yang
	// tampak sah (ADR-009: range/sort hilang permanen untuk field terenkripsi).
	if r.fc != nil {
		if _, encrypted := r.fc.spec(sort); encrypted {
			return "", core.ErrValidation("sort", fmt.Sprintf("kolom %s terenkripsi sehingga tidak bisa diurutkan", sort))
		}
	}
	dir := "ASC"
	switch strings.ToLower(order) {
	case "", "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	default:
		return "", core.ErrValidation("order", "order harus asc atau desc")
	}
	return sort + " " + dir, nil
}

func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case pageSize < 1:
		pageSize = 20
	case pageSize > 100:
		pageSize = 100
	}
	return page, pageSize
}
