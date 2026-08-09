// module_events.go menyambungkan deklarasi event MODUL (Manifest().Events) ke registry schema
// event bus di composition root.
//
// Sebelum ini `eventbus.NewSchemaRegistry()` di run() hanya diisi event identity (PR-5.1.4),
// sementara `Bus.Publish` menolak nama event yang tak terdaftar. Akibatnya SETIAP event yang
// dideklarasikan modul ditolak di gerbang — dan karena use case modul referensi membuang error
// publish (`_ = uc.publisher.Publish(...)`), penolakannya tak menghasilkan 500, tak menghasilkan
// log, tak menghasilkan apa pun. Event hilang sejak baris pertama server hidup (ROADMAP PR-5.1.4
// §GAP (a)).
package main

import (
	"context"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
)

// wireModuleEventSchemas mendaftarkan schema event seluruh modul terdaftar, lalu melaporkan
// subscription yang produsennya tak terpasang.
//
// URUTAN PEMANGGILAN (dijaga run()): SESUDAH `registry.Validate()` dan SEBELUM Bootstrap modul.
//
//   - sesudah Validate, dengan alasan yang sama seperti StrictPermissions: hanya himpunan modul
//     yang koheren (nama unik, DependsOn DAG, entity sah) yang boleh berkontribusi ke state
//     global proses. Validate sendiri TIDAK memeriksa event sama sekali — jadi ia bukan
//     prasyarat teknis, melainkan pilihan agar registry schema tak pernah terisi dari manifest
//     yang sudah dinyatakan tak valid;
//   - sebelum Bootstrap, karena Bootstrap adalah titik pertama modul memegang App dan karenanya
//     titik pertama ia BISA menerbitkan event (mis. seeding). Mendaftarkan sesudahnya berarti
//     ada jendela tempat publish ditolak — dan pada pemanggil yang membuang error, jendela itu
//     tak terlihat.
//
// Registrasi yang gagal MENGGAGALKAN BOOT. Satu-satunya penyebabnya adalah deklarasi manifest
// yang tak koheren (nama kosong, schema nil, atau dua modul mengklaim nama event yang sama
// dengan tipe payload berbeda) — semuanya cacat build-time yang tak akan sembuh sendiri saat
// melayani request, dan yang terakhir berarti dua modul punya arti berbeda untuk satu nama di
// kawat. Ini konsisten dengan perlakuan registry modul, katalog role, dan kripto di run().
func wireModuleEventSchemas(
	ctx context.Context,
	registry *domain.Registry,
	bus *eventbus.Bus,
	logger port.Logger,
) error {
	if err := registry.RegisterEventSchemas(bus.Schema()); err != nil {
		return err
	}
	for _, s := range registry.ExternalSubscriptions() {
		// INFO, bukan WARN — ini keadaan NORMAL, bukan anomali. Consumes memang loose (lihat
		// domain.Registry.ExternalSubscriptions), jadi setiap deployment yang tak memasang
		// produsennya akan menghasilkan baris ini di SETIAP boot. Baris WARN yang selalu muncul
		// berhenti dibaca, dan kejadian yang benar-benar perlu perhatian ikut tenggelam bersamanya.
		// Yang dicari di sini adalah keterlihatan (inventaris saat boot), bukan alarm.
		//
		// CAKUPANNYA SEMPIT, jangan dibaca lebih luas: ia hanya menyatakan "tak ada modul terpasang
		// yang memproduksi event ini". Ia TIDAK membuktikan subscription-nya hidup — `App.Subscribe`
		// membuang error `Bus.Subscribe`, jadi subscribe yang gagal (mis. Flush NATS) tetap
		// menghasilkan consumer tuli tanpa gejala. Itu seam terpisah (core/domain/app.go).
		// Event komponen non-modul juga ikut terhitung di sini meski schema-nya terdaftar lewat
		// registrar sendiri (identity di run(); customization belum dirakit di composition root).
		logger.Info(ctx, "modul men-subscribe event yang tak diproduksi modul terpasang mana pun",
			port.F("module", s.Module),
			port.F("event", s.Event),
			port.F("handler", s.Handler))
	}
	return nil
}
