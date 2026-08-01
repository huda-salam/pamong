//go:build integration

package notification_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreNotif "github.com/huda-salam/pamong/core/notification"
	identsync "github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/testkit/cryptokit"
)

// setupDirectoryDB membuka pool ke DB uji dan membersihkan tabel yang disentuh test PER-TABEL
// (BUKAN drop schema gov — gov dipakai bersama lintas-paket, lihat konvensi tenant_role_integration_test.go).
// Tabel dibuat lewat ensure-on-write oleh repo tenantrole/adapter/db, jadi setup tak perlu
// menerapkan migrasi apa pun.
func setupDirectoryDB(t *testing.T) (*db.Pool, *crypto.Service, context.Context) {
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
	pool := db.NewPool(pgpool)
	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.user_role_assignments`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.tenant_role_permissions`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.tenant_roles`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.org_units`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.user_profiles`)
	}
	drop()
	cryptokit.Cleanup(t, pool)
	t.Cleanup(func() {
		drop()
		cryptokit.Cleanup(t, pool)
		pgpool.Close()
	})
	// Kontak pada clone TERENKRIPSI (PR-3.8.5) dengan realm = tenant, jadi test ini butuh kripto
	// nyata: yang diuji adalah bahwa direktori membuka kontak yang ditulis writer sync — bukan
	// bahwa ia bisa membaca kolom.
	return pool, cryptokit.NewService(t, pool, dirTenant), ctx
}

// dirTenant adalah tenant yang dipakai seluruh test di file ini (RoleTarget.TenantID) —
// sekaligus realm kunci kolom kontak pada clone.
const dirTenant = "pemkot"

// newDirectory merakit direktori pada realm tenant yang sama dengan penulis clone.
func newDirectory(t *testing.T, pool *db.Pool, svc *crypto.Service) *infraNotif.DBRecipientDirectory {
	t.Helper()
	dir, err := infraNotif.NewDBRecipientDirectory(pool, dirTenant, svc)
	if err != nil {
		t.Fatalf("NewDBRecipientDirectory: %v", err)
	}
	return dir
}

// poolSatu memenuhi identsync.TenantPools untuk seed clone lewat writer NYATA.
type poolSatu struct{ pool *db.Pool }

func (p poolSatu) Tenant(context.Context, string) (*db.Pool, error) { return p.pool, nil }

// seedRole membuat satu role tenant lewat repo (ensure-on-write membuat tabel bila belum ada).
func seedRole(t *testing.T, pool *db.Pool, ctx context.Context, name string) uuid.UUID {
	t.Helper()
	role := &domain.TenantRole{ID: uuid.New(), Name: name, Label: name}
	if err := tenantroledb.NewTenantRoleRepo(pool).Save(ctx, role); err != nil {
		t.Fatalf("seed role %q: %v", name, err)
	}
	return role.ID
}

type assignmentSpec struct {
	userID         uuid.UUID
	roleID         uuid.UUID
	unitKerjaID    *uuid.UUID
	includeSubtree bool
	validUntil     *time.Time
}

func seedAssignment(t *testing.T, pool *db.Pool, ctx context.Context, s assignmentSpec) {
	t.Helper()
	a := &domain.TenantRoleAssignment{
		ID:             uuid.New(),
		UserID:         s.userID,
		RoleID:         s.roleID,
		UnitKerjaID:    s.unitKerjaID,
		IncludeSubtree: s.includeSubtree,
		AssignedBy:     uuid.New(),
		ValidFrom:      time.Now().Add(-time.Hour),
		ValidUntil:     s.validUntil,
	}
	if err := tenantroledb.NewTenantRoleAssignmentRepo(pool).Save(ctx, a); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
}

// seedOrgUnits membuat gov.org_units minimal (adjacency list), pola sama abac_integration_test.go.
func seedOrgUnits(t *testing.T, pool *db.Pool, ctx context.Context, units ...struct {
	id, parent uuid.UUID
	name       string
}) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS gov.org_units (id UUID PRIMARY KEY, parent_id UUID REFERENCES gov.org_units(id), name VARCHAR(255) NOT NULL)`); err != nil {
		t.Fatalf("create org_units: %v", err)
	}
	for _, u := range units {
		var parent any
		if u.parent != uuid.Nil {
			parent = u.parent
		}
		if _, err := pool.Exec(ctx, `INSERT INTO gov.org_units (id, parent_id, name) VALUES ($1,$2,$3)`,
			u.id, parent, u.name); err != nil {
			t.Fatalf("seed org_unit %s: %v", u.name, err)
		}
	}
}

// seedUserProfile menulis satu baris clone gov.user_profiles lewat WRITER SYNC yang sebenarnya,
// bukan raw SQL. Sebelum PR-3.8.5 test ini menyalin DDL clone dengan tangan; sejak kontaknya
// terenkripsi, salinan itu tak hanya berisiko menyimpang — ia mustahil (ciphertext-nya harus
// dibuat kunci tenant yang sama). Yang diuji karenanya menjadi SAMBUNGAN penulis ↔ pembaca.
//
// Kontak kosong ("") disimpan NULL oleh writer, sehingga semantik "kosong = kanal tak tersedia"
// tetap yang diuji.
func seedUserProfile(t *testing.T, pool *db.Pool, svc *crypto.Service, ctx context.Context, id uuid.UUID, email, noHP string) {
	t.Helper()
	w, err := identsync.NewTenantDBWriter(poolSatu{pool: pool}, svc)
	if err != nil {
		t.Fatalf("NewTenantDBWriter: %v", err)
	}
	if err := w.Upsert(ctx, dirTenant, identsync.UserProfileClone{
		PersonID: id, AssignmentID: uuid.New(), NIK: "3578010101900001",
		NamaLengkap: "Dummy", EmploymentStatus: "asn", Email: email, NoHP: noHP,
	}); err != nil {
		t.Fatalf("seed user_profile: %v", err)
	}
}

// TestDBRecipientDirectory_HoldersOf_MengisiKontakDariClone membuktikan Email/Phone terisi dari
// clone gov.user_profiles (PR-N3b): holder dengan profil berkontak → terisi; holder tanpa profil
// → kontak kosong (best-effort, tak menggagalkan resolusi pemegang).
func TestDBRecipientDirectory_HoldersOf_MengisiKontakDariClone(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	userBerkontak, userKedua, userTanpaProfil := uuid.New(), uuid.New(), uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userBerkontak, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userKedua, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userTanpaProfil, roleID: roleID})
	seedUserProfile(t, pool, svc, ctx, userBerkontak, "kadis@example.test", "0812340001")
	// Holder KEDUA ber-kontak bukan hiasan: dengan satu baris saja, membuka ciphertext memakai
	// id dari PERMINTAAN (bukan dari baris) kebetulan selalu benar — dan pengikatan baris
	// ADR-016 pada jalur ini jadi tak teruji sama sekali.
	seedUserProfile(t, pool, svc, ctx, userKedua, "sekdis@example.test", "0812340002")

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	byID := map[uuid.UUID]coreNotif.Recipient{}
	for _, r := range got {
		byID[r.PersonID] = r
	}
	if r := byID[userBerkontak]; r.Email != "kadis@example.test" || r.Phone != "0812340001" {
		t.Fatalf("kontak holder tak terisi dari clone: email=%q phone=%q", r.Email, r.Phone)
	}
	if r := byID[userKedua]; r.Email != "sekdis@example.test" || r.Phone != "0812340002" {
		t.Fatalf("kontak holder kedua salah/tak terisi: email=%q phone=%q", r.Email, r.Phone)
	}
	if r := byID[userTanpaProfil]; r.Email != "" || r.Phone != "" {
		t.Fatalf("holder tanpa profil harus kontak kosong, dapat email=%q phone=%q", r.Email, r.Phone)
	}
}

func TestDBRecipientDirectory_HoldersOf_TenantWide(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	userA, userB := uuid.New(), uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userA, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userB, roleID: roleID})

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("holders = %d, mau 2 (tenant-wide, tanpa filter unit)", len(got))
	}
	ids := map[uuid.UUID]bool{got[0].PersonID: true, got[1].PersonID: true}
	if !ids[userA] || !ids[userB] {
		t.Fatalf("holders tak sesuai: %+v", got)
	}
}

// TestDBRecipientDirectory_HoldersOf_DedupUserAssignmentGanda membuktikan satu user dengan DUA
// assignment aktif untuk role yang sama (mis. di-assign ulang dgn scope unit berbeda) hanya
// dihitung SEKALI — tanpa dedup, RoleNotifier akan mengirim notifikasi dobel ke orang yang sama.
func TestDBRecipientDirectory_HoldersOf_DedupUserAssignmentGanda(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	user := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID}) // assignment kedua, user sama

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != user {
		t.Fatalf("holders = %+v, mau tepat 1 (dedup user dgn assignment ganda)", got)
	}
}

func TestDBRecipientDirectory_HoldersOf_ScopeUnit_ExactDanSubtree(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")

	dinas, bidang, lain := uuid.New(), uuid.New(), uuid.New()
	seedOrgUnits(t, pool, ctx,
		struct {
			id, parent uuid.UUID
			name       string
		}{dinas, uuid.Nil, "Dinas"},
		struct {
			id, parent uuid.UUID
			name       string
		}{bidang, dinas, "Bidang"},
		struct {
			id, parent uuid.UUID
			name       string
		}{lain, uuid.Nil, "Lain"},
	)

	userExact := uuid.New()     // assignment persis di target unit (bidang)
	userSubtree := uuid.New()   // assignment di induk (dinas) dengan include_subtree=true
	userNoSubtree := uuid.New() // assignment di induk (dinas) TANPA include_subtree
	userLain := uuid.New()      // assignment di unit lain sama sekali

	seedAssignment(t, pool, ctx, assignmentSpec{userID: userExact, roleID: roleID, unitKerjaID: &bidang})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userSubtree, roleID: roleID, unitKerjaID: &dinas, includeSubtree: true})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userNoSubtree, roleID: roleID, unitKerjaID: &dinas, includeSubtree: false})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userLain, roleID: roleID, unitKerjaID: &lain})

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas", UnitKerjaID: &bidang})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	ids := map[uuid.UUID]bool{}
	for _, r := range got {
		ids[r.PersonID] = true
	}
	if !ids[userExact] {
		t.Error("assignment persis di unit target harus ikut")
	}
	if !ids[userSubtree] {
		t.Error("assignment di induk dgn include_subtree=true harus ikut (subtree)")
	}
	if ids[userNoSubtree] {
		t.Error("assignment di induk TANPA include_subtree TIDAK boleh ikut")
	}
	if ids[userLain] {
		t.Error("assignment di unit lain (di luar subtree) TIDAK boleh ikut")
	}
	if len(got) != 2 {
		t.Fatalf("holders = %d, mau 2 (exact + subtree)", len(got))
	}
}

func TestDBRecipientDirectory_HoldersOf_ExpiredAssignment_Diabaikan(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")

	past := time.Now().Add(-time.Hour)
	user := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID, validUntil: &past})

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("holders = %d, mau 0 (assignment kedaluwarsa harus diabaikan): %+v", len(got), got)
	}
}

func TestDBRecipientDirectory_HoldersOf_TidakFilterCrossTenant(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")

	user := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID})

	// gov.user_profiles: user ini ditandai cross-tenant (mis. PJ Bupati dari Pemprov, di-clone
	// + diberi role tenant). HoldersOf tidak boleh mengecualikannya — query tak menyentuh kolom
	// is_cross_tenant sama sekali (lihat doc DBRecipientDirectory).
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS gov.user_profiles (
		id UUID PRIMARY KEY, person_id UUID NOT NULL, employment_status VARCHAR(10) NOT NULL,
		nip VARCHAR(18), nik VARCHAR(16) NOT NULL, nama_lengkap VARCHAR(255) NOT NULL,
		assignment_id UUID NOT NULL, is_cross_tenant BOOLEAN NOT NULL DEFAULT false,
		synced_at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatalf("create user_profiles: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gov.user_profiles
		(id, person_id, employment_status, nik, nama_lengkap, assignment_id, is_cross_tenant, synced_at)
		VALUES ($1,$1,'asn','1234567890123456','PJ Bupati',$2,true,now())`,
		user, uuid.New()); err != nil {
		t.Fatalf("seed user_profiles: %v", err)
	}

	dir := newDirectory(t, pool, svc)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != user {
		t.Fatalf("holder cross-tenant harus tetap kembali: %+v", got)
	}
}

func TestDBRecipientDirectory_ActingFor_SelaluKosong(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	seedAssignment(t, pool, ctx, assignmentSpec{userID: uuid.New(), roleID: roleID})

	dir := newDirectory(t, pool, svc)
	got, err := dir.ActingFor(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("ActingFor: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ActingFor harus SELALU kosong (PLT-jabatan deferred), dapat: %+v", got)
	}
}

// TestDBRecipientDirectory_EndToEnd_ViaRoleNotifier membuktikan in-app jalan penuh dari DB
// nyata: HoldersOf berisi → notif masuk inbox holder; HoldersOf kosong → ErrNoRecipient
// (fail-loud, sesuai kebijakan Router — jabatan tak bertuan tak boleh hilang diam-diam).
func TestDBRecipientDirectory_EndToEnd_ViaRoleNotifier(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	holder := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: holder, roleID: roleID})

	templates := coreNotif.NewMemoryTemplateStore()
	if err := templates.Upsert(ctx, coreNotif.Template{Key: "surat.disposisi", Locale: "id", Subject: "Disposisi", Body: "Isi disposisi"}); err != nil {
		t.Fatalf("upsert template: %v", err)
	}
	inbox := coreNotif.NewMemoryInAppInbox()
	channels := coreNotif.NewChannelRegistry()
	if err := channels.Register(coreNotif.NewInAppChannel(inbox)); err != nil {
		t.Fatalf("register channel: %v", err)
	}
	hub := coreNotif.NewHub(channels, coreNotif.NewTemplateEngine(templates), coreNotif.NewMemoryDeliveryRecorder())

	dir := newDirectory(t, pool, svc)
	notifier := coreNotif.NewRoleNotifier(coreNotif.NewRouter(dir), hub)

	// Jabatan berpemegang -> in-app terkirim.
	n, err := notifier.NotifyRole(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"},
		"surat.disposisi", map[string]any{}, coreNotif.ChannelInApp)
	if err != nil {
		t.Fatalf("NotifyRole (ada pemegang): %v", err)
	}
	if n != 1 {
		t.Fatalf("count penerima = %d, mau 1", n)
	}
	items, err := inbox.List(ctx, "pemkot", holder.String(), 0)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox holder = %d item, mau 1", len(items))
	}

	// Jabatan tak bertuan (role lain, tanpa assignment) -> ErrNoRecipient, fail-loud.
	_, err = notifier.NotifyRole(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "jabatan_kosong"},
		"surat.disposisi", map[string]any{}, coreNotif.ChannelInApp)
	if err == nil {
		t.Fatal("jabatan tak bertuan harus gagal (ErrNoRecipient), bukan diam-diam sukses")
	}
}

// TestDBRecipientDirectory_KontakDipindahAntarBarisDitolak menguji pengikatan baris (ADR-016)
// pada jalur yang konsekuensinya paling langsung: bila ciphertext kontak bisa dipindah antar
// baris dan tetap terbuka, notifikasi berisi data pribadi seseorang terkirim ke alamat orang
// lain — dan tak ada satu pun error yang menandainya.
//
// Yang dijaga bukan "ada error", melainkan bahwa direktori mengambil identitas baris dari BARIS
// ITU SENDIRI. Kegagalan membuka bersifat best-effort, jadi hasil yang benar adalah kontak
// KOSONG (kanal email/SMS tak tersedia) — bukan kontak yang tertukar.
func TestDBRecipientDirectory_KontakDipindahAntarBarisDitolak(t *testing.T) {
	pool, svc, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	userA, userB := uuid.New(), uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userA, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userB, roleID: roleID})
	seedUserProfile(t, pool, svc, ctx, userA, "a@example.test", "0812340001")
	seedUserProfile(t, pool, svc, ctx, userB, "b@example.test", "0812340002")

	// Tukar email_enc kedua baris lewat SQL langsung — persis yang bisa dilakukan siapa pun yang
	// punya akses tulis ke DB tenant tanpa punya kunci.
	if _, err := pool.Exec(ctx, `
		WITH a AS (SELECT email_enc FROM gov.user_profiles WHERE id = $1),
		     b AS (SELECT email_enc FROM gov.user_profiles WHERE id = $2)
		UPDATE gov.user_profiles p SET email_enc = CASE
		    WHEN p.id = $1 THEN (SELECT email_enc FROM b)
		    ELSE (SELECT email_enc FROM a) END
		WHERE p.id IN ($1, $2)`, userA, userB); err != nil {
		t.Fatalf("tukar email_enc: %v", err)
	}

	got, err := dirHolders(t, newDirectory(t, pool, svc), ctx)
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	for _, r := range got {
		if r.Email != "" {
			t.Fatalf("kontak terbuka meski ciphertext-nya milik baris lain: person=%s email=%q",
				r.PersonID, r.Email)
		}
	}
}

func dirHolders(t *testing.T, dir *infraNotif.DBRecipientDirectory, ctx context.Context) ([]coreNotif.Recipient, error) {
	t.Helper()
	return dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: dirTenant, Role: "kepala_dinas"})
}
