package db

import (
	"context"
	"sync"

	"github.com/huda-salam/pamong/port"
)

// EnsureSchemaLocked menjalankan DDL bootstrap (CREATE SCHEMA/TABLE ... IF NOT EXISTS) di dalam
// SATU transaksi yang memegang advisory lock `schemaBootstrapLock`.
//
// Lock-nya bukan kehati-hatian berlebih: `IF NOT EXISTS` **tidak** membuat DDL Postgres atomik.
// Dua koneksi bisa sama-sama lolos pemeriksaan "belum ada" lalu satu kalah di unique index katalog
// sistem (`pg_namespace_nspname_index` untuk CREATE SCHEMA, `pg_type_typname_nsp_index` untuk
// CREATE TABLE) dengan error 23505 — bukan diabaikan. Selama DDL ini hanya dipanggil saat boot,
// benturan itu jarang; sejak ensure-on-write ikut ke JALUR REQUEST (PR-W3b), ia menjadi dua
// request bersamaan pada tenant baru — dan yang kalah gagal SESUDAH mutasinya commit, sehingga
// baris tersimpan tanpa audit. `ApplyEmbeddedSchema` sudah memakai idiom yang sama; ini
// menyeragamkannya untuk seluruh jalur ensure-on-write.
//
// Menuntut TxConn (bukan Conn): advisory lock hanya bermakna bila terikat transaksi
// (`pg_advisory_xact_lock` lepas otomatis saat commit/rollback). Varian sesi (`pg_advisory_lock`)
// di atas pool salah kamar — tiap Exec bisa memakai koneksi berbeda, jadi lock-nya bocor.
func EnsureSchemaLocked(ctx context.Context, conn TxConn, ddl string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op setelah Commit

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, schemaBootstrapLock); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, ddl); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DBKeyer melaporkan pengenal DATABASE yang dituju sebuah koneksi pada context ini. Ia ada supaya
// memo "skema sudah dipastikan" dikunci dengan hal yang benar: yang menentukan DDL mendarat di mana
// adalah DB-nya, bukan tenant pada payload yang sedang ditulis.
//
// Dua implementasinya berbeda tepat pada titik itu: `*Pool` selalu satu DB (kunci konstan),
// `*TenantRoutingConn` memilih DB dari tenant di context (kunci = tenant). Menyamakan keduanya jadi
// "selalu tenant" membuat repo ber-pool tetap menjalankan DDL sekali per tenant walau DB-nya itu-itu
// juga; menyamakannya jadi "selalu konstan" membuat repo ber-routing hanya memastikan DB tenant
// PERTAMA — dan tenant kedua gagal dengan "relation does not exist".
type DBKeyer interface {
	DBKey(ctx context.Context) string
}

// dbKey memilih kunci memo untuk conn ini. Koneksi yang tak mengimplementasi DBKeyer (mis. fake di
// test) jatuh ke tenant context: berlebihan (DDL bisa terulang per tenant) tapi tak pernah BOLONG —
// arah kegagalan yang benar untuk bootstrap skema.
func dbKey(ctx context.Context, conn Conn) string {
	if k, ok := conn.(DBKeyer); ok {
		return k.DBKey(ctx)
	}
	return port.TenantFrom(ctx)
}

// SchemaMemo mencatat DB mana yang skemanya sudah dipastikan ada pada PROSES ini, sehingga DDL
// ensure-on-write dibayar sekali per DB alih-alih tiap operasi.
//
// Ia sengaja hidup di INSTANCE repo, bukan sebagai variabel paket. Perbedaannya bukan gaya: memo
// se-proses akan bertahan melewati test yang MENGHAPUS tabelnya di antara kasus uji (pola tetap
// integration test repo ini), sehingga kasus berikutnya berjalan di atas tabel yang sudah tak ada.
// Dengan memo per-instance, repo baru = memo baru, dan test tetap jujur. Konsekuensinya: yang
// menginginkan penghematan lintas-request harus menahan INSTANCE-nya hidup (lihat cache per-tenant
// di cmd/server/scoped_evaluator.go), bukan menaikkan memo ke tingkat paket.
//
// Zero value siap pakai.
type SchemaMemo struct {
	mu   sync.Mutex
	done map[string]bool
}

// Ensure menjalankan ddl sekali per DB (lihat DBKeyer), di bawah advisory lock.
//
// Memo ditandai HANYA setelah DDL sukses commit: menandainya lebih dulu akan mengubah kegagalan
// sesaat (DB belum siap) menjadi permanen selama proses hidup — tabel tak pernah dibuat, tapi
// tak ada lagi yang mencoba.
func (m *SchemaMemo) Ensure(ctx context.Context, conn TxConn, ddl string) error {
	key := dbKey(ctx, conn)

	m.mu.Lock()
	done := m.done[key]
	m.mu.Unlock()
	if done {
		return nil
	}

	if err := EnsureSchemaLocked(ctx, conn, ddl); err != nil {
		return err
	}

	m.mu.Lock()
	if m.done == nil {
		m.done = make(map[string]bool)
	}
	m.done[key] = true
	m.mu.Unlock()
	return nil
}
