//go:build integration

package workflow_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
)

func newTestInstanceStore(t *testing.T) (*infraWf.DBInstanceStore, context.Context) {
	t.Helper()
	return newTestInstanceStoreMaxConns(t, 0)
}

// newTestInstanceStoreMaxConns membangun store dengan batas koneksi tertentu. maxConns=0 berarti
// pakai default pgx. Batas 1 dipakai untuk membuktikan kunci transisi tak menahan koneksi.
func newTestInstanceStoreMaxConns(t *testing.T, maxConns int32) (*infraWf.DBInstanceStore, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
		cfg.MinConns = 0
	}
	pgpool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov CASCADE`)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS gov CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	store := infraWf.NewDBInstanceStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return store, ctx
}

// testPool membuka pool terpisah ke DB test — dipakai test yang perlu menyentuh baris
// infrastruktur (mis. memundurkan batas sewa kunci) di luar API store.
func testPool(t *testing.T) *db.Pool {
	t.Helper()
	pgpool, err := pgxpool.New(context.Background(), os.Getenv("PAMONG_TEST_DB_DSN"))
	if err != nil {
		t.Fatalf("buka pool bantu: %v", err)
	}
	t.Cleanup(pgpool.Close)
	return db.NewPool(pgpool)
}

func sampleInstance() *coreWf.WorkflowInstance {
	return &coreWf.WorkflowInstance{
		ID:                uuid.New(),
		TenantID:          "pemkot-surabaya",
		DefinitionID:      "surat_masuk.disposisi.standar",
		DefinitionVersion: 1,
		EntityID:          uuid.New(),
		CurrentState:      "diterima",
		StartedAt:         time.Now().UTC().Truncate(time.Microsecond),
		RoleBindings:      map[string]string{"sekretaris_daerah": "sekda_kota"},
	}
}

// Instance disimpan lalu dibaca utuh — termasuk RoleBindings (yang dibekukan saat
// StartFromTemplate) dan riwayat transisi.
func TestDBInstanceStore_SaveLaluGet(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	inst := sampleInstance()

	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save: %v", err)
	}
	if inst.Version != 1 {
		t.Fatalf("Version setelah save pertama = %d, ingin 1", inst.Version)
	}

	// Transisi: state pindah + satu entri riwayat, lalu disimpan lagi.
	actor := uuid.New()
	inst.CurrentState = "didisposisi"
	inst.History = append(inst.History, coreWf.TransitionRecord{
		From: "diterima", To: "didisposisi", Action: "DisposisiSurat",
		ActorID: actor, Timestamp: time.Now().UTC().Truncate(time.Microsecond),
		Comment: "segera tindak lanjuti",
	})
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save kedua: %v", err)
	}

	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CurrentState != "didisposisi" || got.Version != 2 {
		t.Errorf("state=%q version=%d, ingin didisposisi/2", got.CurrentState, got.Version)
	}
	if got.DefinitionVersion != 1 || got.EntityID != inst.EntityID {
		t.Errorf("versi definisi / entity tidak terjaga: %+v", got)
	}
	if got.RoleBindings["sekretaris_daerah"] != "sekda_kota" {
		t.Errorf("RoleBindings tidak terjaga: %+v", got.RoleBindings)
	}
	if len(got.History) != 1 || got.History[0].ActorID != actor ||
		got.History[0].Comment != "segera tindak lanjuti" {
		t.Errorf("riwayat tidak terjaga: %+v", got.History)
	}
}

// Optimistic locking: dua penulis yang sama-sama membaca versi lama, hanya satu yang menang.
// Tanpa ini, satu surat bisa terdisposisi dua kali dengan hanya satu jejak di riwayat.
func TestDBInstanceStore_SaveBasiDitolak(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	inst := sampleInstance()
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save awal: %v", err)
	}

	// Dua pembaca memegang salinan versi yang sama.
	a, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	b, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}

	a.CurrentState = "didisposisi"
	if err := store.Save(ctx, a); err != nil {
		t.Fatalf("save a (pemenang): %v", err)
	}

	b.CurrentState = "selesai"
	err = store.Save(ctx, b)
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Fatalf("save b = %v, ingin CONFLICT", err)
	}
	if b.Version != 1 {
		t.Errorf("Version pemanggil yang kalah dinaikkan jadi %d; ingin tetap 1", b.Version)
	}

	// State di DB adalah milik pemenang, bukan tertimpa yang kalah.
	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get akhir: %v", err)
	}
	if got.CurrentState != "didisposisi" {
		t.Fatalf("state akhir = %q, ingin didisposisi (hasil pemenang)", got.CurrentState)
	}
}

// CurrentState adalah jalur guard race eskalasi SLA: ia harus melihat state TERKINI.
func TestDBInstanceStore_CurrentState(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	inst := sampleInstance()
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save: %v", err)
	}

	state, err := store.CurrentState(ctx, inst.ID)
	if err != nil || state != "diterima" {
		t.Fatalf("CurrentState = %q, %v; ingin diterima", state, err)
	}

	inst.CurrentState = "selesai"
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save kedua: %v", err)
	}
	if state, err = store.CurrentState(ctx, inst.ID); err != nil || state != "selesai" {
		t.Fatalf("CurrentState setelah transisi = %q, %v; ingin selesai", state, err)
	}
}

func TestDBInstanceStore_GetTidakAda(t *testing.T) {
	store, ctx := newTestInstanceStore(t)

	_, err := store.Get(ctx, uuid.New())

	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("err = %v, ingin NOT_FOUND", err)
	}
}

// --- Regresi review PR-W4a ---

// Satu alur per (definisi, entitas) ditegakkan DB, bukan hanya oleh handler: pemanggil lain
// (importer, CLI, use case modul) tak boleh bisa membuat instance kedua untuk dokumen yang sama.
func TestDBInstanceStore_SatuInstancePerEntitas(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	pertama := sampleInstance()
	if err := store.Save(ctx, pertama); err != nil {
		t.Fatalf("save pertama: %v", err)
	}

	kedua := sampleInstance()
	kedua.EntityID = pertama.EntityID // entitas & definisi sama, instance baru

	err := store.Save(ctx, kedua)

	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Fatalf("save kedua = %v, ingin CONFLICT (bukan 500 dari pelanggaran unique)", err)
	}
}

// Kunci transisi: eksklusif per instance, tak menunggu, dan lepas saat release.
func TestDBInstanceStore_TryLockInstance(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	inst := sampleInstance()
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save: %v", err)
	}

	release, ok, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil || !ok {
		t.Fatalf("kunci pertama: ok=%v err=%v", ok, err)
	}

	// Pemegang kedua ditolak SEKETIKA (bukan mengantre).
	release2, ok2, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("kunci kedua: %v", err)
	}
	if ok2 {
		release2()
		t.Fatal("kunci kedua berhasil; transisi bersamaan tak tersaring")
	}

	// Instance LAIN tidak ikut terkunci.
	lain := sampleInstance()
	if err := store.Save(ctx, lain); err != nil {
		t.Fatalf("save instance lain: %v", err)
	}
	releaseLain, okLain, err := store.TryLockInstance(ctx, lain.ID)
	if err != nil || !okLain {
		t.Fatalf("kunci instance lain: ok=%v err=%v", okLain, err)
	}
	releaseLain()

	// Setelah dilepas, instance semula bisa dikunci lagi — tak ada kunci yatim.
	release()
	release3, ok3, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil || !ok3 {
		t.Fatalf("kunci setelah release: ok=%v err=%v", ok3, err)
	}
	release3()
}

// Kunci transisi TIDAK BOLEH menahan koneksi selama dipegang.
//
// Versi pertama W4a memegang kunci lewat transaksi advisory-lock yang tetap terbuka. Action yang
// berjalan di bawah kunci memakai POOL YANG SAMA, jadi setiap transisi menyandera satu koneksi
// selama use case bekerja: pada tenant sibuk, transisi bersamaan sebanyak ukuran pool sudah cukup
// untuk menggantungkan SELURUH request tenant itu — termasuk yang tak ada hubungannya dengan
// workflow. Test ini memasang pool berisi TEPAT SATU koneksi: bila kunci menahannya, query
// sesudahnya tak akan pernah dapat koneksi dan mati di deadline.
func TestDBInstanceStore_KunciTakMenahanKoneksi(t *testing.T) {
	store, ctx := newTestInstanceStoreMaxConns(t, 1)
	inst := sampleInstance()
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save: %v", err)
	}

	release, ok, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil || !ok {
		t.Fatalf("kunci: ok=%v err=%v", ok, err)
	}
	defer release()

	// Berkerja SELAGI kunci dipegang — inilah yang dilakukan action pada transisi sungguhan.
	kerja, batal := context.WithTimeout(ctx, 5*time.Second)
	defer batal()
	if _, err := store.CurrentState(kerja, inst.ID); err != nil {
		t.Fatalf("query selagi kunci dipegang: %v (kunci menyandera koneksi pool)", err)
	}
}

// Proses yang MATI di tengah transisi tak boleh menyandera instance selamanya. Sewa yang habis
// bisa diambil alih — ini harga yang dibayar untuk melepas koneksi (advisory lock ikut mati
// bersama koneksinya; baris kunci tidak).
func TestDBInstanceStore_KunciKedaluwarsaBisaDiambilAlih(t *testing.T) {
	store, ctx := newTestInstanceStore(t)
	inst := sampleInstance()
	if err := store.Save(ctx, inst); err != nil {
		t.Fatalf("save: %v", err)
	}

	release, ok, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil || !ok {
		t.Fatalf("kunci pertama: ok=%v err=%v", ok, err)
	}
	_ = release // sengaja TIDAK dilepas: mensimulasikan proses yang mati memegang kunci

	// Majukan waktu dengan memundurkan batas sewa — setara menunggu TTL habis.
	if _, err := testPool(t).Exec(ctx,
		`UPDATE gov.workflow_instance_locks SET locked_until = now() - interval '1 second'
		 WHERE instance_id = $1`, inst.ID); err != nil {
		t.Fatalf("kedaluwarsakan sewa: %v", err)
	}

	release2, ok2, err := store.TryLockInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("kunci kedua: %v", err)
	}
	if !ok2 {
		t.Fatal("kunci kedaluwarsa tak bisa diambil alih; instance tersandera selamanya")
	}
	release2()
}
