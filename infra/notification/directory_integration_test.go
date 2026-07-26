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
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// setupDirectoryDB membuka pool ke DB uji dan membersihkan tabel yang disentuh test PER-TABEL
// (BUKAN drop schema gov — gov dipakai bersama lintas-paket, lihat konvensi tenant_role_integration_test.go).
// Tabel dibuat lewat ensure-on-write oleh repo tenantrole/adapter/db, jadi setup tak perlu
// menerapkan migrasi apa pun.
func setupDirectoryDB(t *testing.T) (*db.Pool, context.Context) {
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
	t.Cleanup(func() {
		drop()
		pgpool.Close()
	})
	return pool, ctx
}

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

func TestDBRecipientDirectory_HoldersOf_TenantWide(t *testing.T) {
	pool, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	userA, userB := uuid.New(), uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userA, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: userB, roleID: roleID})

	dir := infraNotif.NewDBRecipientDirectory(pool)
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
	pool, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	user := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID})
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID}) // assignment kedua, user sama

	dir := infraNotif.NewDBRecipientDirectory(pool)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != user {
		t.Fatalf("holders = %+v, mau tepat 1 (dedup user dgn assignment ganda)", got)
	}
}

func TestDBRecipientDirectory_HoldersOf_ScopeUnit_ExactDanSubtree(t *testing.T) {
	pool, ctx := setupDirectoryDB(t)
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

	dir := infraNotif.NewDBRecipientDirectory(pool)
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
	pool, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")

	past := time.Now().Add(-time.Hour)
	user := uuid.New()
	seedAssignment(t, pool, ctx, assignmentSpec{userID: user, roleID: roleID, validUntil: &past})

	dir := infraNotif.NewDBRecipientDirectory(pool)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("holders = %d, mau 0 (assignment kedaluwarsa harus diabaikan): %+v", len(got), got)
	}
}

func TestDBRecipientDirectory_HoldersOf_TidakFilterCrossTenant(t *testing.T) {
	pool, ctx := setupDirectoryDB(t)
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

	dir := infraNotif.NewDBRecipientDirectory(pool)
	got, err := dir.HoldersOf(ctx, coreNotif.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"})
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != user {
		t.Fatalf("holder cross-tenant harus tetap kembali: %+v", got)
	}
}

func TestDBRecipientDirectory_ActingFor_SelaluKosong(t *testing.T) {
	pool, ctx := setupDirectoryDB(t)
	roleID := seedRole(t, pool, ctx, "kepala_dinas")
	seedAssignment(t, pool, ctx, assignmentSpec{userID: uuid.New(), roleID: roleID})

	dir := infraNotif.NewDBRecipientDirectory(pool)
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
	pool, ctx := setupDirectoryDB(t)
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

	dir := infraNotif.NewDBRecipientDirectory(pool)
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
