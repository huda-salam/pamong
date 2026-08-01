package db

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// pegawai adalah entity uji ber-field terenkripsi: nik (personal_id, searchable) di samping
// kolom biasa. Belum ada entity produksi yang punya pengenal, jadi jalur ini diuji di sini.
type pegawai struct {
	ID      uuid.UUID
	Nama    string
	NIK     string
	Version int
}

type pegawaiMapper struct{}

func (pegawaiMapper) Table() string           { return "kepegawaian.pegawais" }
func (pegawaiMapper) DataColumns() []string   { return []string{"nama", "nik"} }
func (pegawaiMapper) SearchColumns() []string { return []string{"nama"} }
func (pegawaiMapper) DataValues(e *pegawai) []any {
	return []any{e.Nama, e.NIK}
}
func (pegawaiMapper) Scan(s RowScanner) (*pegawai, error) {
	var p pegawai
	return &p, s.Scan(&p.ID, &p.Nama, &p.NIK, &p.Version)
}
func (pegawaiMapper) ID(e *pegawai) uuid.UUID      { return e.ID }
func (pegawaiMapper) Version(e *pegawai) int       { return e.Version }
func (pegawaiMapper) SetVersion(e *pegawai, v int) { e.Version = v }

func pegawaiDef() domain.EntityDef {
	return domain.EntityDef{
		Name: "Pegawai", Schema: "kepegawaian", Tier: domain.Tier1,
		Audit: domain.NotAudited{Reason: "uji"}, Lockable: domain.NotLockable{},
		Fields: []domain.FieldDef{
			{Name: "nama", Type: domain.FieldText, Class: domain.DataPersonal},
			{Name: "nik", Type: domain.FieldText, Class: domain.DataPersonalID, Searchable: true},
		},
	}
}

// --- fake DBConn: merekam SQL & argumen, mengembalikan baris yang disiapkan test ---

type recordedQuery struct {
	sql  string
	args []any
}

type fakeCryptoConn struct {
	queries []recordedQuery
	execs   []recordedQuery
	row     port.Row
}

func (c *fakeCryptoConn) QueryRow(_ context.Context, sql string, args ...any) port.Row {
	c.queries = append(c.queries, recordedQuery{sql, args})
	return c.row
}

func (c *fakeCryptoConn) Query(_ context.Context, sql string, args ...any) (port.Rows, error) {
	c.queries = append(c.queries, recordedQuery{sql, args})
	return nil, context.Canceled // List dihentikan sebelum iterasi — SQL-nya yang diuji
}

func (c *fakeCryptoConn) Exec(_ context.Context, sql string, args ...any) (port.CommandTag, error) {
	c.execs = append(c.execs, recordedQuery{sql, args})
	return fakeTag{1}, nil
}

type fakeTag struct{ n int64 }

func (t fakeTag) RowsAffected() int64 { return t.n }

// scriptedRow mengembalikan nilai yang sudah disiapkan ke dest Scan.
type scriptedRow struct {
	values []any
	err    error
}

func (r scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch dd := d.(type) {
		case *uuid.UUID:
			*dd = r.values[i].(uuid.UUID)
		case *string:
			*dd = r.values[i].(string)
		case *int:
			*dd = r.values[i].(int)
		case *[]byte:
			b, _ := r.values[i].([]byte)
			*dd = b
		}
	}
	return nil
}

func cryptoRepo(t *testing.T, conn port.DBConn) *SQLRepository[pegawai] {
	t.Helper()
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(testkit.NewMockCrypto(), FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("NewSQLRepository: %v", err)
	}
	return repo
}

func tenantCtx() context.Context {
	return port.WithTenant(context.Background(), "pemkot-surabaya")
}

func TestFieldCryptoFromEntity_HanyaClassTerenkripsi(t *testing.T) {
	specs := FieldCryptoFromEntity(pegawaiDef())
	if len(specs) != 1 {
		t.Fatalf("jumlah spec = %d, mau 1 (hanya nik; nama class personal TIDAK dienkripsi)", len(specs))
	}
	if specs[0].Column != "nik" || specs[0].Purpose != "nik" || !specs[0].Searchable {
		t.Fatalf("spec = %+v", specs[0])
	}
}

func TestSave_KolomTerenkripsiJadiEncDanBidx(t *testing.T) {
	conn := &fakeCryptoConn{}
	repo := cryptoRepo(t, conn)

	p := &pegawai{ID: uuid.New(), Nama: "Budi", NIK: "3578010101010001"}
	if err := repo.Save(tenantCtx(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(conn.execs) != 1 {
		t.Fatalf("jumlah exec = %d", len(conn.execs))
	}
	sql := conn.execs[0].sql
	for _, want := range []string{"nik_enc", "nik_bidx"} {
		if !strings.Contains(sql, want) {
			t.Errorf("INSERT tidak memuat %s: %s", want, sql)
		}
	}
	if strings.Contains(sql, " nik,") {
		t.Errorf("INSERT masih menulis kolom plaintext nik: %s", sql)
	}
	// Nilai NIK tak boleh muncul mentah di argumen mana pun.
	for i, a := range conn.execs[0].args {
		if s, ok := a.(string); ok && s == "3578010101010001" {
			t.Fatalf("argumen %d memuat NIK plaintext", i)
		}
	}
	// nama (class personal) TETAP plaintext — enkripsi selektif, bukan "semua PII".
	var adaNama bool
	for _, a := range conn.execs[0].args {
		if s, ok := a.(string); ok && s == "Budi" {
			adaNama = true
		}
	}
	if !adaNama {
		t.Error("nama seharusnya tetap tersimpan plaintext (class personal)")
	}
}

func TestFindByID_MendekripsiKolomTerenkripsi(t *testing.T) {
	mock := testkit.NewMockCrypto()
	id := uuid.New()
	ct, err := mock.Encrypt(tenantCtx(), port.FieldRef{
		TenantID: "pemkot-surabaya", Purpose: "nik", RecordID: id.String(),
	}, []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("siapkan ciphertext: %v", err)
	}
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{id, "Budi", ct, 1}}}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	got, err := repo.FindByID(tenantCtx(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.NIK != "3578010101010001" {
		t.Fatalf("nik = %q, mau plaintext hasil dekripsi", got.NIK)
	}
	if got.Nama != "Budi" {
		t.Fatalf("nama = %q", got.Nama)
	}
	if !strings.Contains(conn.queries[0].sql, "nik_enc") {
		t.Errorf("SELECT harus membaca nik_enc: %s", conn.queries[0].sql)
	}
}

// TestFindByID_TolakCiphertextPindahKolom menutup batas yang TIDAK bisa ditegakkan AAD
// (lihat crypto.fieldAAD): blob ber-purpose lain yang disalin ke kolom ini harus ditolak.
func TestFindByID_TolakCiphertextPindahKolom(t *testing.T) {
	mock := testkit.NewMockCrypto()
	id := uuid.New()
	// Sengaja BARIS YANG SAMA: AAD-nya identik (ADR-016 tak terpicu), jadi yang harus
	// menangkap perpindahan ini semata-mata pemeriksaan purpose ADR-015.
	asing, err := mock.Encrypt(tenantCtx(), port.FieldRef{
		TenantID: "pemkot-surabaya", Purpose: "no_rekening", RecordID: id.String(),
	}, []byte("1234567890"))
	if err != nil {
		t.Fatalf("siapkan ciphertext: %v", err)
	}
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{id, "Budi", asing, 1}}}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	_, err = repo.FindByID(tenantCtx(), id)
	if err == nil || !strings.Contains(err.Error(), "dipindah antar kolom") {
		t.Fatalf("err = %v, mau penolakan purpose tak cocok", err)
	}
}

// --- ADR-016: pengikatan baris ---

// TestFindByID_TolakCiphertextPindahBaris menutup sisa risiko yang dicatat ADR-015: blob
// yang dipindah ke BARIS lain lolos pemeriksaan purpose (purpose-nya benar) dan hanya
// tertangkap oleh AAD ber-record_id.
func TestFindByID_TolakCiphertextPindahBaris(t *testing.T) {
	mock := testkit.NewMockCrypto()
	budi, siti := uuid.New(), uuid.New()
	ctSiti, err := mock.Encrypt(tenantCtx(), port.FieldRef{
		TenantID: "pemkot-surabaya", Purpose: "nik", RecordID: siti.String(),
	}, []byte("3578010101010002"))
	if err != nil {
		t.Fatalf("siapkan ciphertext: %v", err)
	}

	// Baris Budi, tapi nik_enc milik Siti — hasil `UPDATE ... SET nik_enc = (SELECT ...)`.
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{budi, "Budi", ctSiti, 1}}}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	got, err := repo.FindByID(tenantCtx(), budi)
	if err == nil {
		t.Fatalf("nik milik baris lain harus GAGAL dibaca, malah dapat %+v", got)
	}
	if !strings.Contains(err.Error(), "dekripsi kolom nik") {
		t.Fatalf("err = %v, mau kegagalan dekripsi karena pengikatan baris", err)
	}
}

// TestSave_CiphertextTerikatBarisYangMenulisnya mengunci sisi TULIS: nilai yang disimpan
// tak boleh bisa dibuka dengan id baris lain. Tanpa ini, pengikatan bisa "hilang" di jalur
// tulis tanpa satu pun test baca yang gagal.
func TestSave_CiphertextTerikatBarisYangMenulisnya(t *testing.T) {
	mock := testkit.NewMockCrypto()
	conn := &fakeCryptoConn{}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	p := &pegawai{ID: uuid.New(), Nama: "Budi", NIK: "3578010101010001"}
	if err := repo.Save(tenantCtx(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ct, ok := conn.execs[0].args[2].([]byte) // urutan: id, nama, nik_enc, nik_bidx
	if !ok {
		t.Fatalf("argumen nik_enc bertipe %T, mau []byte", conn.execs[0].args[2])
	}

	if _, err := mock.Decrypt(tenantCtx(), port.RowRef{
		TenantID: "pemkot-surabaya", RecordID: uuid.New().String(),
	}, ct); !errors.Is(err, port.ErrCiphertextInvalid) {
		t.Fatalf("ciphertext harus terikat baris penulisnya, dibuka baris lain err = %v", err)
	}
	got, err := mock.Decrypt(tenantCtx(), port.RowRef{
		TenantID: "pemkot-surabaya", RecordID: p.ID.String(),
	}, ct)
	if err != nil || string(got) != "3578010101010001" {
		t.Fatalf("baris sendiri harus tetap bisa membuka: %q, %v", got, err)
	}
}

// TestSave_EntityTanpaIDDitolak: id nil berarti nilai terikat ke "baris kosong" dan karena
// itu dapat dipindah ke baris kosong mana pun. Harus gagal sebelum menyentuh DB.
func TestSave_EntityTanpaIDDitolak(t *testing.T) {
	conn := &fakeCryptoConn{}
	repo := cryptoRepo(t, conn)

	err := repo.Save(tenantCtx(), &pegawai{Nama: "Budi", NIK: "3578010101010001"})
	if err == nil || !strings.Contains(err.Error(), "tanpa id") {
		t.Fatalf("err = %v, mau penolakan entity tanpa id", err)
	}
	if len(conn.execs) != 0 {
		t.Fatal("tidak boleh ada INSERT saat entity tak ber-id")
	}
}

// TestList_BlindIndexTidakTerikatBaris menjaga syarat ADR-016 §3. Kalau blind index ikut
// ber-baris, nilainya akan berbeda tiap pemanggilan dan `WHERE nik_bidx = $1` tak akan
// pernah cocok — pencarian & UNIQUE mati diam-diam, bukan gagal lantang.
func TestList_BlindIndexTidakTerikatBaris(t *testing.T) {
	mock := testkit.NewMockCrypto()
	conn := &fakeCryptoConn{}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	const nik = "3578010101010001"
	// Dua baris berbeda, nilai NIK sama → blind index WAJIB sama.
	for _, id := range []uuid.UUID{uuid.New(), uuid.New()} {
		if err := repo.Save(tenantCtx(), &pegawai{ID: id, Nama: "Budi", NIK: nik}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	b1, _ := conn.execs[0].args[3].([]byte)
	b2, _ := conn.execs[1].args[3].([]byte)
	if len(b1) == 0 || !bytes.Equal(b1, b2) {
		t.Fatalf("blind index harus row-independent: %x vs %x", b1, b2)
	}

	// Dan nilai yang dicari lewat filter harus cocok dengan yang tersimpan.
	conn.row = scriptedRow{values: []any{int64(0)}}
	_, _ = repo.List(tenantCtx(), port.ListFilter{Filters: map[string]any{"nik": nik}})
	bFilter, _ := conn.queries[0].args[0].([]byte)
	if !bytes.Equal(b1, bFilter) {
		t.Fatalf("blind index filter (%x) tak cocok dengan yang disimpan (%x) — lookup tak akan pernah menemukan baris", bFilter, b1)
	}
}

// pegawaiStrID meniru mapper Tier 3 tulis tangan yang menyimpan id sebagai string — bentuk
// yang sah menurut kontrak Mapper (ID() tetap mengembalikan uuid.UUID) dan lolos semua jalur
// non-kripto. Tanpa dukungan di scannedID, entity seperti ini gagal dibaca SELURUHNYA begitu
// ia mendeklarasikan satu field terenkripsi.
type pegawaiStrID struct {
	ID      string
	Nama    string
	NIK     string
	Version int
}

type pegawaiStrIDMapper struct{}

func (pegawaiStrIDMapper) Table() string           { return "kepegawaian.pegawais" }
func (pegawaiStrIDMapper) DataColumns() []string   { return []string{"nama", "nik"} }
func (pegawaiStrIDMapper) SearchColumns() []string { return []string{"nama"} }
func (pegawaiStrIDMapper) DataValues(e *pegawaiStrID) []any {
	return []any{e.Nama, e.NIK}
}
func (pegawaiStrIDMapper) Scan(s RowScanner) (*pegawaiStrID, error) {
	var p pegawaiStrID
	return &p, s.Scan(&p.ID, &p.Nama, &p.NIK, &p.Version)
}
func (pegawaiStrIDMapper) ID(e *pegawaiStrID) uuid.UUID {
	id, _ := uuid.Parse(e.ID)
	return id
}
func (pegawaiStrIDMapper) Version(e *pegawaiStrID) int       { return e.Version }
func (pegawaiStrIDMapper) SetVersion(e *pegawaiStrID, v int) { e.Version = v }

// TestFindByID_IDBukanUUIDStructTetapTerbaca mengunci lebar kontrak Mapper: id boleh
// di-scan sebagai string. Ejaan non-kanonik (huruf besar) ikut diuji karena AAD dibangun dari
// TEKS id — tanpa kanonikalisasi, baris yang ditulis dengan ejaan berbeda tak bisa membuka
// nilainya sendiri.
func TestFindByID_IDBukanUUIDStructTetapTerbaca(t *testing.T) {
	mock := testkit.NewMockCrypto()
	id := uuid.New()
	ct, err := mock.Encrypt(tenantCtx(), port.FieldRef{
		TenantID: "pemkot-surabaya", Purpose: "nik", RecordID: id.String(),
	}, []byte("3578010101010001"))
	if err != nil {
		t.Fatalf("siapkan ciphertext: %v", err)
	}

	for _, ejaan := range []string{id.String(), strings.ToUpper(id.String())} {
		conn := &fakeCryptoConn{row: scriptedRow{values: []any{ejaan, "Budi", ct, 1}}}
		repo, err := NewSQLRepository[pegawaiStrID](conn, pegawaiStrIDMapper{},
			WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
		if err != nil {
			t.Fatalf("repo: %v", err)
		}
		got, err := repo.FindByID(tenantCtx(), id)
		if err != nil {
			t.Fatalf("id ejaan %q: FindByID: %v", ejaan, err)
		}
		if got.NIK != "3578010101010001" {
			t.Fatalf("id ejaan %q: nik = %q", ejaan, got.NIK)
		}
	}
}

// TestFindByID_GagalDekripsiMenyebutBaris: kegagalan satu baris menggagalkan seluruh List
// (keputusan sengaja, lihat List). Karena itu errornya WAJIB menyebut id baris — tanpa itu
// operator tak punya cara menemukan baris rusak selain memindai tabel.
func TestFindByID_GagalDekripsiMenyebutBaris(t *testing.T) {
	mock := testkit.NewMockCrypto()
	budi, siti := uuid.New(), uuid.New()
	ctSiti, err := mock.Encrypt(tenantCtx(), port.FieldRef{
		TenantID: "pemkot-surabaya", Purpose: "nik", RecordID: siti.String(),
	}, []byte("3578010101010002"))
	if err != nil {
		t.Fatalf("siapkan ciphertext: %v", err)
	}
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{budi, "Budi", ctSiti, 1}}}
	repo, err := NewSQLRepository[pegawai](conn, pegawaiMapper{},
		WithFieldCrypto(mock, FieldCryptoFromEntity(pegawaiDef())))
	if err != nil {
		t.Fatalf("repo: %v", err)
	}

	_, err = repo.FindByID(tenantCtx(), budi)
	if err == nil {
		t.Fatal("harus gagal")
	}
	if !strings.Contains(err.Error(), budi.String()) {
		t.Fatalf("err = %v, harus menyebut id baris %s", err, budi)
	}
	if strings.Contains(err.Error(), "3578010101010002") {
		t.Fatalf("err membocorkan plaintext: %v", err)
	}
}

func TestList_FilterTerenkripsiLewatBlindIndex(t *testing.T) {
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{int64(0)}}}
	repo := cryptoRepo(t, conn)

	_, _ = repo.List(tenantCtx(), port.ListFilter{Filters: map[string]any{"nik": "3578010101010001"}})

	if len(conn.queries) == 0 {
		t.Fatal("tidak ada query")
	}
	sql := conn.queries[0].sql
	if !strings.Contains(sql, "nik_bidx = $1") {
		t.Fatalf("filter harus dialihkan ke blind index: %s", sql)
	}
	if b, ok := conn.queries[0].args[0].([]byte); !ok || len(b) == 0 {
		t.Fatalf("argumen filter = %T, mau blind index []byte", conn.queries[0].args[0])
	}
}

func TestList_SortKolomTerenkripsiDitolak(t *testing.T) {
	conn := &fakeCryptoConn{row: scriptedRow{values: []any{int64(0)}}}
	repo := cryptoRepo(t, conn)

	_, err := repo.List(tenantCtx(), port.ListFilter{Sort: "nik"})
	if err == nil || !strings.Contains(err.Error(), "tidak bisa diurutkan") {
		t.Fatalf("err = %v, mau penolakan sort atas kolom terenkripsi", err)
	}
}

func TestOperasiTanpaTenant_Gagal(t *testing.T) {
	conn := &fakeCryptoConn{}
	repo := cryptoRepo(t, conn)

	err := repo.Save(context.Background(), &pegawai{ID: uuid.New(), NIK: "3578010101010001"})
	if err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("err = %v, mau gagal karena tenant tak diketahui", err)
	}
	if len(conn.execs) != 0 {
		t.Fatal("tidak boleh ada INSERT saat tenant tak diketahui")
	}
}

func TestNilaiKosong_DisimpanNULLBukanCiphertext(t *testing.T) {
	conn := &fakeCryptoConn{}
	repo := cryptoRepo(t, conn)

	if err := repo.Save(tenantCtx(), &pegawai{ID: uuid.New(), Nama: "Budi", NIK: ""}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	args := conn.execs[0].args
	// urutan: id, nama, nik_enc, nik_bidx
	if args[2] != nil || args[3] != nil {
		t.Fatalf("nik kosong harus NULL di _enc & _bidx, dapat %v / %v", args[2], args[3])
	}
}

func TestNewSQLRepository_TolakSpecKolomTakDikenal(t *testing.T) {
	_, err := NewSQLRepository[pegawai](&fakeCryptoConn{}, pegawaiMapper{},
		WithFieldCrypto(testkit.NewMockCrypto(), []FieldCryptoSpec{{Column: "nik_salah_ketik"}}))
	if err == nil {
		t.Fatal("spec dengan nama kolom tak dikenal harus ditolak (kalau lolos, field tersimpan plaintext diam-diam)")
	}
}

func TestNewRepository_EntityTerenkripsiTanpaCryptoDitolak(t *testing.T) {
	_, err := NewRepository[pegawai](nil, pegawaiMapper{}, pegawaiDef(), nil)
	if err == nil || !strings.Contains(err.Error(), "CryptoPort") {
		t.Fatalf("err = %v, mau penolakan karena CryptoPort tak diberikan", err)
	}
}
