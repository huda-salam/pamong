//go:build integration

package sync_test

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/crypto"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit/cryptokit"
	"github.com/jackc/pgx/v5/pgxpool"
)

// itTenant adalah tenant tujuan clone — sekaligus REALM kunci kolom terenkripsinya.
const itTenant = "pemkot-surabaya"

// fakePools mengembalikan satu pool DB uji untuk tenant apa pun — cukup untuk menguji
// jalur tulis writer terhadap Postgres nyata tanpa registry/TenantConnManager penuh.
type fakePools struct{ pool *infradb.Pool }

func (f fakePools) Tenant(_ context.Context, _ string) (*infradb.Pool, error) { return f.pool, nil }

// setupTenantDB membuka pool ke DB uji dan membersihkan schema gov.
func setupTenantDB(t *testing.T) (*infradb.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := infradb.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.user_profiles`)
		cryptokit.Cleanup(t, pool)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS gov.user_profiles`); err != nil {
		t.Fatalf("reset gov: %v", err)
	}
	cryptokit.Cleanup(t, pool)
	return pool, ctx
}

// newWriter merakit writer dengan kripto NYATA. DB uji berperan ganda: ia identity DB (tempat
// id.data_keys) sekaligus tenant DB (tempat clone) — pemisahan fisiknya bukan yang diuji di sini.
func newWriter(t *testing.T, pool *infradb.Pool) (*sync.TenantDBWriter, *crypto.Service) {
	t.Helper()
	svc := cryptokit.NewService(t, pool, itTenant)
	w, err := sync.NewTenantDBWriter(fakePools{pool: pool}, svc)
	if err != nil {
		t.Fatalf("NewTenantDBWriter: %v", err)
	}
	return w, svc
}

// TestSyncClone_EmploymentDitugaskan adalah DoD PR-2.2.4: event identity.employment.ditugaskan
// menghasilkan clone di gov.user_profiles tenant tujuan (lewat memory bus).
func TestSyncClone_EmploymentDitugaskan(t *testing.T) {
	pool, ctx := setupTenantDB(t)

	writer, svc := newWriter(t, pool)
	// Pengenal datang dari CloneSource, bukan dari event (PR-3.8.5b). Nilai fixture-nya sama
	// dengan yang diperiksa di bawah, sehingga yang diuji tetap jalur tulis ujung-ke-ujung.
	engine := newEngine(t, writer, newSource())
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}

	personID := uuid.New()
	assignmentID := uuid.New()
	if err := bus.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: "pemkot-surabaya",
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID:     assignmentID,
			EmploymentID:     uuid.New(),
			PersonID:         personID,
			TenantID:         "pemkot-surabaya",
			NamaLengkap:      "Budi Santoso",
			EmploymentStatus: "asn",
		},
	}); err != nil {
		t.Fatalf("publish ditugaskan: %v", err)
	}

	// Clone harus muncul di gov.user_profiles tenant. Pengenal dibaca dari kolom _enc lalu
	// dibuka — kolom plaintext-nya sudah tak ada (lihat TestSyncClone_KolomPlaintextTakAda).
	var (
		gotNama, gotStatus string
		gotAssignment      uuid.UUID
		gotCross           bool
		nikEnc, nipEnc     []byte
		emailEnc, noHPEnc  []byte
		nikBidx, nipBidx   []byte
	)
	if err := pool.QueryRow(ctx,
		`SELECT nik_enc, nik_bidx, nip_enc, nip_bidx, nama_lengkap, employment_status,
		        assignment_id, is_cross_tenant, email_enc, no_hp_enc
		   FROM gov.user_profiles WHERE id = $1`, personID,
	).Scan(&nikEnc, &nikBidx, &nipEnc, &nipBidx, &gotNama, &gotStatus,
		&gotAssignment, &gotCross, &emailEnc, &noHPEnc); err != nil {
		t.Fatalf("clone tidak ditemukan: %v", err)
	}
	if gotNama != "Budi Santoso" || gotStatus != "asn" || gotAssignment != assignmentID || gotCross {
		t.Fatalf("data clone salah: nama=%s status=%s assignment=%s cross=%v",
			gotNama, gotStatus, gotAssignment, gotCross)
	}

	sealer, err := crypto.NewFieldSealer(svc, itTenant, "test")
	if err != nil {
		t.Fatalf("NewFieldSealer: %v", err)
	}
	for _, c := range []struct {
		purpose, mau string
		enc          []byte
	}{
		{"nik", "3578010101900001", nikEnc},
		{"nip", "199001012015011001", nipEnc},
		// Kontak ikut ter-clone (PR-N3b) → sumber Recipient.Email/Phone.
		{"email", "budi@example.test", emailEnc},
		{"no_hp", "0812340001", noHPEnc},
	} {
		// Identitas baris untuk AAD dari baris itu sendiri (personID = kolom id).
		got, err := sealer.Open(ctx, c.purpose, personID, c.enc)
		if err != nil {
			t.Fatalf("buka %s: %v", c.purpose, err)
		}
		if got != c.mau {
			t.Fatalf("%s clone = %q, mau %q", c.purpose, got, c.mau)
		}
	}

	// Blind index harus cocok dengan yang dihitung pembaca dari nilai pencarian — inilah yang
	// menopang ResolveByNIK/NIP setelah kolom plaintext hilang.
	for _, c := range []struct {
		purpose, nilai string
		bidx           []byte
	}{
		{"nik", "3578010101900001", nikBidx},
		{"nip", "199001012015011001", nipBidx},
	} {
		mau, err := sealer.Index(ctx, c.purpose, c.nilai)
		if err != nil {
			t.Fatalf("index %s: %v", c.purpose, err)
		}
		if string(mau) != string(c.bidx) {
			t.Fatalf("%s_bidx tak cocok dengan hasil hitung pembaca — lookup mati tanpa error", c.purpose)
		}
	}

	// Idempoten: event terkirim ulang tidak menggandakan baris (ON CONFLICT).
	if err := bus.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: "pemkot-surabaya",
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID: assignmentID, EmploymentID: uuid.New(), PersonID: personID,
			TenantID: "pemkot-surabaya", NamaLengkap: "Budi Santoso", EmploymentStatus: "asn",
		},
	}); err != nil {
		t.Fatalf("publish ulang: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gov.user_profiles WHERE id = $1`, personID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("clone harus idempoten (1 baris), dapat %d", count)
	}
}

// TestSyncClone_KolomPlaintextTakAda adalah DoD PR-3.8.5 untuk jalur clone: pengenal bukan
// sekadar BERHENTI DIISI, kolomnya HILANG. Membiarkan kolom plaintext berdampingan berarti
// dump tenant tetap terbaca — dan seluruh pekerjaan ini hanya memindahkan kebocoran.
func TestSyncClone_KolomPlaintextTakAda(t *testing.T) {
	pool, ctx := setupTenantDB(t)
	writer, _ := newWriter(t, pool)

	if err := writer.Upsert(ctx, itTenant, sync.UserProfileClone{
		PersonID: uuid.New(), AssignmentID: uuid.New(), NIK: "3578010101900002",
		NamaLengkap: "Sri Wahyuni", EmploymentStatus: "non_asn", Email: "sri@example.test",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for _, kolom := range []string{"nik", "nip", "email", "no_hp"} {
		var ada bool
		if err := pool.QueryRow(ctx,
			`SELECT count(*) > 0 FROM information_schema.columns
			   WHERE table_schema = 'gov' AND table_name = 'user_profiles' AND column_name = $1`,
			kolom).Scan(&ada); err != nil {
			t.Fatalf("cek kolom %s: %v", kolom, err)
		}
		if ada {
			t.Fatalf("kolom plaintext %q masih ada di gov.user_profiles", kolom)
		}
	}
}

// TestSyncClone_DumpBersihDariPengenal menguji properti yang sebenarnya dijanjikan ADR-009:
// yang tersimpan di DISK tak memuat pengenal. Ia membaca seluruh baris sebagai teks — persis
// yang dilihat orang yang memegang dump — bukan kolom yang kebetulan diperiksa satu per satu.
func TestSyncClone_DumpBersihDariPengenal(t *testing.T) {
	pool, ctx := setupTenantDB(t)
	writer, _ := newWriter(t, pool)

	const (
		nik   = "3578010101900003"
		nip   = "199001012015011003"
		email = "rahasia@example.test"
		noHP  = "0812340003"
	)
	personID := uuid.New()
	if err := writer.Upsert(ctx, itTenant, sync.UserProfileClone{
		PersonID: personID, AssignmentID: uuid.New(), NIK: nik, NIP: nip,
		NamaLengkap: "Agus Salim", EmploymentStatus: "asn", Email: email, NoHP: noHP,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	var dump string
	if err := pool.QueryRow(ctx,
		`SELECT user_profiles::text FROM gov.user_profiles WHERE id = $1`, personID).
		Scan(&dump); err != nil {
		t.Fatalf("baca baris sebagai teks: %v", err)
	}
	dump = strings.ToLower(dump)
	for _, rahasia := range []string{nik, nip, email, noHP} {
		if strings.Contains(dump, strings.ToLower(rahasia)) {
			t.Fatalf("pengenal %q muncul plaintext di baris clone", rahasia)
		}
		// Kolom _enc bertipe bytea, dan Postgres merendernya sebagai hex (\x33...). Tanpa
		// pemeriksaan bentuk hex ini, nilai yang disimpan MENTAH ke kolom _enc akan lolos —
		// test-nya hijau sementara dump-nya tetap terbaca oleh siapa pun yang men-decode hex.
		if strings.Contains(dump, hex.EncodeToString([]byte(rahasia))) {
			t.Fatalf("pengenal %q tersimpan mentah di kolom bytea (terbaca sebagai hex)", rahasia)
		}
	}
	// Kontrol negatif: nama SENGAJA plaintext (kelas `personal`, harus bisa di-LIKE/ORDER BY).
	// Tanpa ini, test di atas tetap hijau seandainya query-nya salah baris atau kolom kosong.
	if !strings.Contains(dump, strings.ToLower("Agus Salim")) {
		t.Fatal("nama_lengkap tak ditemukan — test membaca baris yang salah, bukan membuktikan apa pun")
	}
}

// TestSyncClone_RealmKunciTenantBukanSentral mengunci keputusan realm: clone dilindungi kunci
// TENANT-nya, bukan kunci sentral identity. Realm sentral di sini berarti satu kunci membuka
// clone seluruh pemda sekaligus — dump satu tenant menjadi tuas untuk semua.
func TestSyncClone_RealmKunciTenantBukanSentral(t *testing.T) {
	pool, ctx := setupTenantDB(t)
	writer, _ := newWriter(t, pool)

	if err := writer.Upsert(ctx, itTenant, sync.UserProfileClone{
		PersonID: uuid.New(), AssignmentID: uuid.New(), NIK: "3578010101900004",
		NamaLengkap: "Dewi Lestari", EmploymentStatus: "non_asn",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM id.data_keys`)
	if err != nil {
		t.Fatalf("baca id.data_keys: %v", err)
	}
	defer rows.Close()
	var realms []string
	for rows.Next() {
		var realm string
		if err := rows.Scan(&realm); err != nil {
			t.Fatalf("scan realm: %v", err)
		}
		realms = append(realms, realm)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterasi realm: %v", err)
	}
	if len(realms) == 0 {
		t.Fatal("tak ada kunci terbentuk — writer tak menyentuh kripto sama sekali")
	}
	for _, realm := range realms {
		if realm == crypto.RealmCentral {
			t.Fatal("clone tenant memakai realm SENTRAL — satu kunci membuka clone semua pemda")
		}
		if realm != itTenant {
			t.Fatalf("realm kunci clone = %q, mau %q", realm, itTenant)
		}
	}
}
