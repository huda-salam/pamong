//go:build integration

package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/testkit"
)

// DoD PR-3.8.6 (ADR-009 §2, ADR-015, ADR-016, ADR-017) di DB NYATA.
//
// Unit test tak bisa membuktikan apa pun di sini: yang dipertaruhkan adalah bentuk tabel,
// UNIQUE yang ditegakkan Postgres, dan isi dump — semuanya properti DB, bukan properti Go.
// Tabel dibangun dari file migrasi nyata (setupIdentityDB), jadi yang diuji adalah SAMBUNGAN
// migrasi ↔ repo, bukan repo sendirian.

const (
	nikBudi = "3578010101900001"
	nipBudi = "199001012015011001"
)

// seedBudi menyimpan satu person ASN lengkap (person + employment + credential) dan
// mengembalikan ketiganya.
func seedBudi(t *testing.T, ctx context.Context, persons *db.PersonRepo, employments *db.EmploymentRepo,
	credentials *db.CredentialRepo) (*domain.Person, *domain.Employment, *domain.Credential) {
	t.Helper()
	p := &domain.Person{
		ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Budi Santoso",
		NoHP: "081234567890", Email: "budi@example.go.id", IsActive: true,
	}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}
	e := &domain.Employment{
		ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN,
		NIP: nipBudi, InstansiAsal: "Pemkot Surabaya", IsActive: true,
	}
	if err := employments.Save(ctx, e); err != nil {
		t.Fatalf("save employment: %v", err)
	}
	c := &domain.Credential{
		ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail,
		CredValue: p.Email, IsPrimary: true,
	}
	if err := credentials.Save(ctx, c); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	return p, e, c
}

// TestIdentityCrypto_KolomPlaintextTidakAda mengunci BENTUK tabel: kolom pengenal plaintext
// harus benar-benar hilang, bukan sekadar berhenti ditulis. Kolom yang masih ada akan
// mengundang query baru yang mengisinya — dan tak ada test lain yang akan protes.
func TestIdentityCrypto_KolomPlaintextTidakAda(t *testing.T) {
	pool, _, ctx := setupIdentityDB(t)

	for _, tc := range []struct {
		table  string
		hilang []string
		wajib  []string
	}{
		{"persons", []string{"nik", "no_hp", "email"},
			[]string{"nik_enc", "nik_bidx", "no_hp_enc", "no_hp_bidx", "email_enc", "email_bidx"}},
		{"employments", []string{"nip"}, []string{"nip_enc", "nip_bidx"}},
		{"credentials", []string{"cred_value"}, []string{"cred_value_enc", "cred_value_bidx"}},
	} {
		cols := map[string]bool{}
		rows, err := pool.Query(ctx,
			`SELECT column_name FROM information_schema.columns
			 WHERE table_schema = 'id' AND table_name = $1`, tc.table)
		if err != nil {
			t.Fatalf("katalog %s: %v", tc.table, err)
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan katalog: %v", err)
			}
			cols[c] = true
		}
		rows.Close()

		for _, c := range tc.hilang {
			if cols[c] {
				t.Errorf("id.%s masih punya kolom plaintext %q", tc.table, c)
			}
		}
		for _, c := range tc.wajib {
			if !cols[c] {
				t.Errorf("id.%s tak punya kolom %q", tc.table, c)
			}
		}
	}
}

// TestIdentityCrypto_PengenalTersimpanTerenkripsi membaca kolom fisik lewat SQL mentah —
// melewati repo sepenuhnya, karena repo-lah yang sedang diuji. Inilah yang dilihat seseorang
// yang memegang dump identity DB tanpa kunci.
func TestIdentityCrypto_PengenalTersimpanTerenkripsi(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)
	p, e, c := seedBudi(t, ctx, persons, employments, credentials)

	var nikEnc, nikBidx, noHPEnc, emailEnc []byte
	if err := pool.QueryRow(ctx,
		`SELECT nik_enc, nik_bidx, no_hp_enc, email_enc FROM id.persons WHERE id = $1`, p.ID).
		Scan(&nikEnc, &nikBidx, &noHPEnc, &emailEnc); err != nil {
		t.Fatalf("baca kolom fisik person: %v", err)
	}
	for name, blob := range map[string][]byte{
		"nik_enc": nikEnc, "nik_bidx": nikBidx, "no_hp_enc": noHPEnc, "email_enc": emailEnc,
	} {
		if len(blob) == 0 {
			t.Fatalf("%s kosong — nilai tak tersimpan", name)
		}
	}
	if strings.Contains(string(nikEnc), nikBudi) || strings.Contains(string(nikBidx), nikBudi) {
		t.Fatal("NIK plaintext muncul di kolom nik_enc/nik_bidx")
	}
	if strings.Contains(string(emailEnc), "budi@example.go.id") {
		t.Fatal("email plaintext muncul di kolom email_enc")
	}

	var nipEnc []byte
	if err := pool.QueryRow(ctx, `SELECT nip_enc FROM id.employments WHERE id = $1`, e.ID).
		Scan(&nipEnc); err != nil {
		t.Fatalf("baca nip_enc: %v", err)
	}
	if strings.Contains(string(nipEnc), nipBudi) {
		t.Fatal("NIP plaintext muncul di kolom nip_enc")
	}

	var credEnc []byte
	if err := pool.QueryRow(ctx, `SELECT cred_value_enc FROM id.credentials WHERE id = $1`, c.ID).
		Scan(&credEnc); err != nil {
		t.Fatalf("baca cred_value_enc: %v", err)
	}
	if strings.Contains(string(credEnc), "budi@example.go.id") {
		t.Fatal("nilai credential plaintext muncul di kolom cred_value_enc")
	}
}

// TestIdentityCrypto_ResolveLewatBlindIndex adalah DoD utama: jalur login & resolusi tetap
// jalan sesudah pengenalnya terenkripsi. Kolom _enc mustahil dipakai equality (nonce acak),
// jadi yang membuat ini bekerja adalah _bidx.
func TestIdentityCrypto_ResolveLewatBlindIndex(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)
	p, e, c := seedBudi(t, ctx, persons, employments, credentials)

	gotP, err := persons.FindByNIK(ctx, nikBudi)
	if err != nil {
		t.Fatalf("FindByNIK: %v", err)
	}
	if gotP.ID != p.ID || gotP.NIK != nikBudi || gotP.NoHP != "081234567890" || gotP.Email != "budi@example.go.id" {
		t.Fatalf("person hasil resolve tidak utuh: %+v", gotP)
	}

	gotE, err := employments.FindByNIP(ctx, nipBudi)
	if err != nil {
		t.Fatalf("FindByNIP: %v", err)
	}
	if gotE.ID != e.ID || gotE.NIP != nipBudi {
		t.Fatalf("employment hasil resolve tidak utuh: %+v", gotE)
	}

	gotC, err := credentials.FindByTypeValue(ctx, domain.CredEmail, "budi@example.go.id")
	if err != nil {
		t.Fatalf("FindByTypeValue: %v", err)
	}
	if gotC.ID != c.ID || gotC.PersonID != p.ID || gotC.CredValue != "budi@example.go.id" {
		t.Fatalf("credential hasil resolve tidak utuh: %+v", gotC)
	}

	// Nilai yang tak ada tetap not-found (bukan diam-diam mengembalikan baris lain).
	if _, err := persons.FindByNIK(ctx, "3578010101900999"); err == nil {
		t.Fatal("NIK tak dikenal harus not-found")
	}
}

// TestIdentityCrypto_PurposeKredensialDariCredType mengunci ADR-017 §4: purpose kredensial
// diturunkan dari cred_type, sehingga kredensial email ikut kebijakan normalisasi framework
// (crypto.caseFoldedPurposes). Dua akibat yang dikunci di sini adalah satu keputusan yang
// sama, dan keduanya perubahan semantik yang disengaja:
//
//  1. login lewat email menjadi case-insensitive;
//  2. UNIQUE mulai menangkap "Budi@x.id" vs "budi@x.id" sebagai duplikat.
//
// Bila purpose kembali digabung jadi "cred_value", keduanya mati diam-diam — tanpa error,
// hanya pengguna yang gagal login dan akun kembar yang lolos masuk.
func TestIdentityCrypto_PurposeKredensialDariCredType(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)
	p, _, c := seedBudi(t, ctx, persons, employments, credentials)

	got, err := credentials.FindByTypeValue(ctx, domain.CredEmail, "  BUDI@Example.GO.ID  ")
	if err != nil {
		t.Fatalf("email beda kapitalisasi harus tetap ketemu: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("credential salah: %+v", got)
	}

	var fe *core.FrameworkError
	err = credentials.Save(ctx, &domain.Credential{
		ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail, CredValue: "BUDI@EXAMPLE.GO.ID",
	})
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Fatalf("email kembar beda kapitalisasi harus CONFLICT, dapat: %v", err)
	}

	// oauth SENGAJA tidak ikut di-case-fold: subject dari provider bersifat opaque.
	for _, v := range []string{"Sub-ABC", "sub-abc"} {
		if err := credentials.Save(ctx, &domain.Credential{
			ID: uuid.New(), PersonID: p.ID, CredType: domain.CredOAuth, CredValue: v,
		}); err != nil {
			t.Fatalf("subject oauth %q harus diterima (case-sensitive): %v", v, err)
		}
	}
}

// TestIdentityCrypto_UniqueDitegakkanDiBlindIndex adalah setengah lainnya dari DoD. UNIQUE
// tak mungkin hidup di kolom _enc (nonce acak membuat dua nilai sama tampak berbeda), jadi
// bila pemindahannya ke _bidx gagal, duplikat NIK/NIP masuk tanpa satu pun error.
func TestIdentityCrypto_UniqueDitegakkanDiBlindIndex(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)
	p, _, _ := seedBudi(t, ctx, persons, employments, credentials)

	assertConflict := func(what string, err error) {
		t.Helper()
		var fe *core.FrameworkError
		if err == nil {
			t.Fatalf("%s duplikat harus ditolak", what)
		}
		if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
			t.Fatalf("%s duplikat harus CONFLICT, dapat: %v", what, err)
		}
	}

	assertConflict("NIK", persons.Save(ctx, &domain.Person{
		ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Penyusup", IsActive: true,
	}))
	assertConflict("NIP", employments.Save(ctx, &domain.Employment{
		ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN, NIP: nipBudi, IsActive: true,
	}))
	assertConflict("credential (tipe+nilai)", credentials.Save(ctx, &domain.Credential{
		ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail, CredValue: "budi@example.go.id",
	}))

	// Nilai SAMA pada tipe BERBEDA tetap boleh — keunikan majemuk (cred_type, cred_value_bidx)
	// masih majemuk sesudah kolomnya berpindah. cred_type sengaja tetap plaintext.
	if err := credentials.Save(ctx, &domain.Credential{
		ID: uuid.New(), PersonID: p.ID, CredType: domain.CredNoHP, CredValue: "budi@example.go.id",
	}); err != nil {
		t.Fatalf("nilai sama pada cred_type berbeda harus diterima: %v", err)
	}
}

// Pesan error adalah jalur samping ADR-009 §6: ia mengalir ke log terpusat dan (untuk
// FrameworkError) ke body HTTP. Mengenkripsi kolom sambil menyalin NIK ke teks error hanya
// memindahkan kebocorannya — pengenal tetap mendarat plaintext di tempat yang lebih mudah
// dibaca daripada dump DB.
func TestIdentityCrypto_PesanErrorTanpaPengenal(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)
	p, _, _ := seedBudi(t, ctx, persons, employments, credentials)

	const emailBudi = "budi@example.go.id"
	assertBersih := func(what, rahasia string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: harus error", what)
		}
		if strings.Contains(err.Error(), rahasia) {
			t.Fatalf("%s: pengenal %q bocor ke pesan error: %v", what, rahasia, err)
		}
	}

	// Tak ditemukan — nilai yang dicari tak boleh ikut dikutip.
	_, err := persons.FindByNIK(ctx, "3578019999999999")
	assertBersih("FindByNIK", "3578019999999999", err)
	_, err = employments.FindByNIP(ctx, "199912319999129999")
	assertBersih("FindByNIP", "199912319999129999", err)
	_, err = credentials.FindByTypeValue(ctx, domain.CredEmail, "asing@example.go.id")
	assertBersih("FindByTypeValue", "asing@example.go.id", err)

	// Konflik keunikan — nilai duplikatnya pun tak boleh ikut.
	assertBersih("Save NIK duplikat", nikBudi, persons.Save(ctx, &domain.Person{
		ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Penyusup", IsActive: true,
	}))
	assertBersih("Save NIP duplikat", nipBudi, employments.Save(ctx, &domain.Employment{
		ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN, NIP: nipBudi, IsActive: true,
	}))
	assertBersih("Save credential duplikat", emailBudi, credentials.Save(ctx, &domain.Credential{
		ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail, CredValue: emailBudi,
	}))
}

// TestIdentityCrypto_CiphertextDipindahAntarBarisDitolak — ADR-016 di realm sentral.
// Menukar NIK dua orang lewat SQL langsung tidak boleh menghasilkan pembacaan yang bersih;
// kalau lolos, NIK seseorang terbaca sebagai milik orang lain (atribusi palsu).
func TestIdentityCrypto_CiphertextDipindahAntarBarisDitolak(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)

	budi := &domain.Person{ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Budi", IsActive: true}
	siti := &domain.Person{ID: uuid.New(), NIK: "3578010101900002", NamaLengkap: "Siti", IsActive: true}
	for _, p := range []*domain.Person{budi, siti} {
		if err := persons.Save(ctx, p); err != nil {
			t.Fatalf("save %s: %v", p.NamaLengkap, err)
		}
	}

	// Hanya _enc yang dipindah: menukar _bidx sekaligus akan ditolak DB sendiri (UNIQUE
	// nik_bidx bentrok di tengah statement), jadi yang tersisa untuk diuji adalah persis
	// serangan yang mungkin.
	if _, err := pool.Exec(ctx,
		`UPDATE id.persons a SET nik_enc = b.nik_enc FROM id.persons b
		 WHERE a.id = $1 AND b.id = $2`, budi.ID, siti.ID); err != nil {
		t.Fatalf("pindah ciphertext: %v", err)
	}

	if _, err := persons.FindByID(ctx, budi.ID); err == nil {
		t.Fatal("ciphertext dari baris lain harus GAGAL dibuka (ADR-016), bukan terbaca bersih")
	}
}

// TestIdentityCrypto_CiphertextDipindahAntarKolomDitolak — ADR-015 di realm sentral. AAD
// tidak mengikat kolom (purpose dibaca dari blob), jadi yang menangkap ini hanya pemeriksaan
// PurposeOf di lapis repo. Tanpa itu, no_hp seseorang terbaca sebagai email-nya.
func TestIdentityCrypto_CiphertextDipindahAntarKolomDitolak(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)

	p := &domain.Person{
		ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Budi",
		NoHP: "081234567890", Email: "budi@example.go.id", IsActive: true,
	}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}

	// Baris yang SAMA, kolom berbeda → AAD identik, tag GCM sah. Hanya purpose yang beda.
	if _, err := pool.Exec(ctx,
		`UPDATE id.persons SET email_enc = no_hp_enc WHERE id = $1`, p.ID); err != nil {
		t.Fatalf("pindah antar kolom: %v", err)
	}

	_, err := persons.FindByID(ctx, p.ID)
	if err == nil {
		t.Fatal("ciphertext dari kolom lain harus DITOLAK (ADR-015)")
	}
	if !strings.Contains(err.Error(), "purpose") {
		t.Fatalf("penolakan harus menyebut purpose (bukan sekadar 'blob rusak'): %v", err)
	}
}

// TestIdentityCrypto_KunciRealmSentralTanpaBarisRegistry adalah DoD ADR-017 §3: realm
// sentral memperoleh kunci TANPA baris apa pun di id.tenant_registry.
//
// Ini bukan detail: DBCustodyResolver fail-closed untuk identitas yang tak terdaftar, jadi
// tanpa dekorator WithCentralRealm seluruh jalur ini gagal. Dan bila kelak seseorang
// "memperbaikinya" dengan menyisipkan baris tenant palsu, test ini yang akan protes.
func TestIdentityCrypto_KunciRealmSentralTanpaBarisRegistry(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)

	if err := persons.Save(ctx, &domain.Person{
		ID: uuid.New(), NIK: nikBudi, NamaLengkap: "Budi", IsActive: true,
	}); err != nil {
		t.Fatalf("save person: %v", err)
	}

	var registryRows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM id.tenant_registry`).Scan(&registryRows); err != nil {
		t.Fatalf("hitung registry: %v", err)
	}
	if registryRows != 0 {
		t.Fatalf("realm sentral tak boleh menuntut baris di id.tenant_registry, dapat %d baris", registryRows)
	}

	// DEK realm sentral memang terbentuk, dan HANYA untuk realm sentral.
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT tenant_id, purpose, kind, custody FROM id.data_keys ORDER BY tenant_id, purpose, kind`)
	if err != nil {
		t.Fatalf("baca data_keys: %v", err)
	}
	defer rows.Close()

	var found int
	for rows.Next() {
		var realm, purpose, kind, custody string
		if err := rows.Scan(&realm, &purpose, &kind, &custody); err != nil {
			t.Fatalf("scan data_keys: %v", err)
		}
		if realm != crypto.RealmCentral {
			t.Fatalf("kunci data identity harus di realm %q, dapat %q", crypto.RealmCentral, realm)
		}
		if custody != "platform" {
			t.Fatalf("custody realm sentral wajib platform (ADR-017 §3), dapat %q", custody)
		}
		found++
	}
	if found == 0 {
		t.Fatal("tak ada DEK terbentuk — enkripsi tidak benar-benar berjalan")
	}
}

// TestIdentityCrypto_AuditDiffBersihDariPengenal menutup REVIEW_BACKLOG E2 pada jalur
// employment (NIP), pelengkap jalur person (NIK) di audit_integration_test.go.
//
// Yang diperiksa adalah DUMP KOLOM diff apa adanya — bukan hasil baca lewat repo — karena
// yang dikhawatirkan justru orang yang membaca tabelnya langsung. Hash chain wajib tetap
// verify: penyegelan terjadi SEBELUM entry dirantai, jadi bila ia menyentuh urutan atau isi
// setelah hashing, integritas audit ikut rusak.
func TestIdentityCrypto_AuditDiffBersihDariPengenal(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditStore)
	persons := mustAuditedPersonRepo(t, mustPersonRepo(t, pool, cr), engine, cr)
	employments := mustAuditedEmploymentRepo(t, mustEmploymentRepo(t, pool, cr), engine, cr)

	actor := uuid.New()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermPersonBuat),
		testkit.WithPermission(domain.PermEmploymentLampir),
	)
	pub := testkit.NewMockPublisher()
	p, err := usecase.NewCreatePerson(persons, pub).Execute(actx, usecase.CreatePersonInput{
		NIK: nikBudi, NamaLengkap: "Budi", NoHP: "081234567890", Email: "budi@example.go.id",
	})
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	if _, err := usecase.NewAttachEmployment(persons, employments, pub).Execute(actx,
		usecase.AttachEmploymentInput{PersonID: p.ID, Status: domain.StatusASN, NIP: nipBudi}); err != nil {
		t.Fatalf("attach employment: %v", err)
	}

	var dump string
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(string_agg(diff::text, ' '), '') FROM id.audit_logs`).Scan(&dump); err != nil {
		t.Fatalf("dump diff: %v", err)
	}
	for _, secret := range []string{nikBudi, nipBudi, "081234567890", "budi@example.go.id"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("pengenal %q masih MENTAH di id.audit_logs.diff — E2 belum tertutup", secret)
		}
	}
	// Nama lengkap TIDAK terenkripsi (class personal, harus dapat dicari) — memastikan
	// test di atas benar-benar membedakan, bukan sekadar lolos karena diff kosong.
	if !strings.Contains(dump, "Budi") {
		t.Fatal("diff audit kehilangan nilai non-sensitif — bukti audit ikut terbuang")
	}

	chain, err := auditStore.Chain(ctx)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("harus 2 entry audit identity, dapat %d", len(chain))
	}
	if res := audit.VerifyChain(chain); !res.OK {
		t.Fatalf("hash chain harus tetap utuh sesudah penyegelan: %+v", res)
	}
	// Partisi chain = realm sentral (ADR-017 §2), bukan string "central" lama yang bisa
	// bertabrakan dengan tenant_id sah.
	for _, e := range chain {
		if e.TenantID != crypto.RealmCentral {
			t.Fatalf("partisi chain audit identity harus %q, dapat %q", crypto.RealmCentral, e.TenantID)
		}
	}
}

// TestIdentityCrypto_RepoTanpaCryptoPortDitolak: konstruktor gagal LANTANG. Kalau ia
// membiarkan nil lewat, pengenal mendarat plaintext tanpa satu pun gejala.
func TestIdentityCrypto_RepoTanpaCryptoPortDitolak(t *testing.T) {
	pool, cr, _ := setupIdentityDB(t)

	if _, err := db.NewPersonRepo(pool, nil); err == nil {
		t.Fatal("NewPersonRepo tanpa CryptoPort harus ditolak")
	}
	if _, err := db.NewEmploymentRepo(pool, nil); err == nil {
		t.Fatal("NewEmploymentRepo tanpa CryptoPort harus ditolak")
	}
	if _, err := db.NewCredentialRepo(pool, nil); err == nil {
		t.Fatal("NewCredentialRepo tanpa CryptoPort harus ditolak")
	}

	engine := audit.NewEngine(db.NewAuditStore(pool))
	inner := mustPersonRepo(t, pool, cr)
	if _, err := db.NewAuditedPersonRepo(inner, engine, nil); err == nil {
		t.Fatal("NewAuditedPersonRepo tanpa CryptoPort harus ditolak (diff memuat NIK)")
	}
	if _, err := db.NewAuditedEmploymentRepo(mustEmploymentRepo(t, pool, cr), engine, nil); err == nil {
		t.Fatal("NewAuditedEmploymentRepo tanpa CryptoPort harus ditolak (diff memuat NIP)")
	}
}
