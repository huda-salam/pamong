// identity_sync.go merakit clone engine identity (identity/sync) di composition root.
//
// Tanpa perakitan ini seluruh mesin PR-3.8.5a/3.8.5b tidak pernah jalan di server hidup:
// `gov.user_profiles` tak pernah terisi, jadi `UserResolver` (yang SUDAH ter-wire di run())
// selalu menjawab "tidak ditemukan" dan routing notifikasi tak punya penerima. Yang menutup
// jarak itu hanya tiga potong: subscriber (Engine) + penulis clone (TenantDBWriter) + pembaca
// balik identity (RepoCloneSource).
//
// Semua di sini murni WIRING: tak ada satu pun aturan bisnis yang ditulis ulang. Kebijakan
// kripto ada di crypto.FieldSealer, kebijakan clone di identity/sync.
package main

import (
	"fmt"

	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitysync "github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// wireIdentitySync merakit clone engine dan men-subscribe-nya ke bus.
//
// Empat pilihan perakitan yang tidak sembarang:
//
//  1. Repo identity dipakai APA ADANYA (bukan dekorator audit `NewAuditedPersonRepo`). Jalur
//     clone hanya MEMBACA identity; membungkusnya dengan dekorator mutasi hanya menambah
//     dependency (audit.Engine) tanpa satu pun entry yang akan ditulis.
//
//  2. Repo dirakit di atas identityPool dengan cryptoSvc yang sama seperti sisa identity:
//     merekalah yang membuka `{f}_enc` ber-realm SENTRAL (ADR-017). Itu justru alasan
//     CloneSource ditempatkan di sisi identity — kunci realm sentral tak pernah menyeberang
//     ke penulis clone.
//
//  3. Writer memakai `pools` (TenantConnManager) dan menyegel ulang dengan realm TENANT.
//     Realm yang salah di sini TIDAK gagal — ia hanya membuat `nik_bidx` tak pernah cocok
//     dengan yang dihitung pembaca (`infra/user`), jadi clone tertulis tapi `ResolveByNIK`
//     mati tanpa gejala. Karena itu ia dikunci oleh test, bukan oleh komentar.
//
//  4. cryptoSvc yang sama disuntik ke keduanya. Konstruktor sync menolak CryptoPort nil, jadi
//     salah rakit gagal saat boot — bukan saat baris pertama disalin plaintext.
//
// KEGAGALAN HANDLER: driver NATS Core tak punya re-delivery — error yang dikembalikan handler
// hanya DICATAT (infra/eventbus/nats.go). Gagal baca identity atau gagal tulis clone karena itu
// berarti tenant kehilangan satu baris clone sampai penugasannya diterbitkan ulang. Itu tetap
// lebih benar daripada menulis clone cacat, tapi jangan membacanya sebagai "nanti di-retry".
//
// HANDLER LAMBAT: satu subscription NATS dijalankan SERIAL, dan handler menerima context tanpa
// deadline. Satu tenant DB yang tak terjangkau (dial tanpa connect_timeout, atau ALTER
// ensure-on-write yang antre di belakang transaksi panjang) menahan callback-nya, dan event untuk
// tenant sehat mengantre di belakangnya sampai batas pending klien — lalu dibuang diam-diam.
// TIDAK ditambal dengan timeout per-handler di sini: pada transport tanpa re-delivery, handler
// yang DIBATALKAN kehilangan pesannya sama persis dengan handler yang MACET, jadi timeout hanya
// menukar satu mode kehilangan dengan yang lain sambil membuat kerja lambat-tapi-berhasil gagal.
// Yang benar-benar menyelesaikannya sama dengan butir di atas: consumer durable ber-ack dengan
// dispatch konkuren. DEFERRED(Phase-3.1.x): JetStream durable consumer (ack eksplisit +
// MaxDeliver) atau job rekonsiliasi clone; keduanya belum ada dan tak diadakan di composition root.
func wireIdentitySync(
	identityPool *db.Pool,
	pools identitysync.TenantPools,
	crypto port.CryptoPort,
	sub port.EventSubscriber,
) error {
	personRepo, err := identitydb.NewPersonRepo(identityPool, crypto)
	if err != nil {
		return fmt.Errorf("repo person untuk clone source: %w", err)
	}
	employmentRepo, err := identitydb.NewEmploymentRepo(identityPool, crypto)
	if err != nil {
		return fmt.Errorf("repo employment untuk clone source: %w", err)
	}
	source, err := identitysync.NewRepoCloneSource(personRepo, employmentRepo)
	if err != nil {
		return fmt.Errorf("clone source (baca-balik identity, ADR-018): %w", err)
	}
	writer, err := identitysync.NewTenantDBWriter(pools, crypto)
	if err != nil {
		return fmt.Errorf("penulis clone tenant (realm tenant, ADR-017): %w", err)
	}
	engine, err := identitysync.NewEngine(writer, source)
	if err != nil {
		return fmt.Errorf("clone engine identity: %w", err)
	}
	// Subscribe SEBELUM server melayani request (dipanggil dari run() sebelum ListenAndServe).
	// `NATSDriver.Subscribe` blokir sampai server mencatat SUB, jadi tak ada jendela tempat event
	// yang sudah ter-dispatch hilang tanpa jejak (pitfall infra/eventbus).
	//
	// SISI SHUTDOWN: Engine memang tak melahirkan goroutine sendiri, tapi handler-nya DIJALANKAN
	// di goroutine milik driver. Tanpa penutupan, `run()` yang kembali setelah `srv.Shutdown` akan
	// menjalankan defer `connMgr.Close()`/`identityPool.Close()` di bawah kaki handler yang masih
	// berjalan, dan pesan yang sudah di-buffer klien tapi belum diproses hilang — NATS Core tak
	// me-redeliver, jadi baris clone-nya hilang diam-diam. Karena itu `run()` memanggil
	// `bus.Drain()` SETELAH srv.Shutdown dan SEBELUM defer penutup pool.
	return engine.Register(sub)
}
