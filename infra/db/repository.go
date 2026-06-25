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
}

// NewSQLRepository membuat repository generik. Identifier tabel & kolom divalidasi
// di sini agar tidak ada celah injeksi lewat nama kolom dinamis (sort/filter).
func NewSQLRepository[T any](conn port.DBConn, m Mapper[T]) (*SQLRepository[T], error) {
	if !tableRe.MatchString(m.Table()) {
		return nil, fmt.Errorf("nama tabel tidak valid: %q", m.Table())
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
	cols := append([]string{"id"}, m.DataColumns()...)
	cols = append(cols, "version")
	return &SQLRepository[T]{
		conn:        conn,
		m:           m,
		selectCols:  strings.Join(cols, ", "),
		allowedCols: allowed,
	}, nil
}

var _ port.BaseRepository[struct{}] = (*SQLRepository[struct{}])(nil)

func (r *SQLRepository[T]) FindByID(ctx context.Context, id uuid.UUID) (*T, error) {
	sql := fmt.Sprintf(
		"SELECT %s FROM %s WHERE id = $1 AND deleted_at IS NULL",
		r.selectCols, r.m.Table(),
	)
	entity, err := r.m.Scan(r.conn.QueryRow(ctx, sql, id))
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
	data := r.m.DataColumns()
	cols := append([]string{"id"}, data...)
	cols = append(cols, "version", "created_at", "updated_at")

	ph := make([]string, 0, len(data)+1)
	for i := range data {
		ph = append(ph, fmt.Sprintf("$%d", i+2)) // $1 dipakai id
	}
	values := append([]any{r.m.ID(entity)}, r.m.DataValues(entity)...)

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
	data := r.m.DataColumns()
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
	args := append(r.m.DataValues(entity), r.m.ID(entity), r.m.Version(entity))

	var newVersion int
	err := r.conn.QueryRow(ctx, sql, args...).Scan(&newVersion)
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
		e, err := r.m.Scan(rows)
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
