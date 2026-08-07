package domain

import "fmt"

// EventSchemaRegistrar adalah subset eventbus.SchemaRegistry (metode Register) — seam agar
// core/domain tak mengimport infra (linter domain-no-infra-import). eventbus.SchemaRegistry
// memenuhinya. Bentuknya sama persis dengan identity/domain.EventSchemaRegistrar dan
// core/customization.EventSchemaRegistrar; ketiganya sengaja tak berbagi tipe supaya tak ada
// paket domain yang bergantung pada paket domain lain hanya demi satu interface sebaris.
type EventSchemaRegistrar interface {
	Register(name string, schema any) error
}

// RegisterEventSchemas mendaftarkan SELURUH EventManifest.Produces modul terdaftar ke registry
// schema event bus. **WAJIB dipanggil saat wiring**, sebelum modul di-Bootstrap: `Bus.Publish`
// menolak nama event yang tak terdaftar, jadi tanpa ini setiap event modul mati di gerbang —
// dan karena beberapa use case membuang error publish, kematiannya tanpa satu pun gejala
// (bukan 500, bukan log). Ini pasangan lintas-modul dari registrar per-komponen yang sudah ada
// (identity/domain, core/customization); bedanya, di sini daftarnya tidak ditulis tangan di
// composition root melainkan diturunkan dari manifest — sumber kebenaran yang sama yang dibaca
// registry untuk entity, permission, dan dependency.
//
// Agregasinya hidup di Registry, bukan sebagai loop di cmd/server, dengan alasan yang sama
// seperti StrictPermissions: pengetahuan "apa yang dideklarasikan seluruh modul" adalah milik
// registry, dan menaruhnya di sini membuat pesan gagal bisa menyebut MODUL mana yang bertanggung
// jawab — sesuatu yang hilang bila registry hanya menyerahkan []EventDef ke pemanggil.
//
// KEGAGALAN = GAGAL BOOT (fail-fast, philosophy #4). Dua modul yang mendeklarasikan nama event
// sama dengan tipe payload berbeda ditolak SchemaRegistry (ErrConflict) dan error itu diteruskan
// apa adanya, hanya dibungkus atribusi modul. Aturan nama→tipe sengaja TIDAK diduplikasi di sini:
// satu-satunya pemegangnya tetap SchemaRegistry, dan karena pendaftarannya menumpang registry
// yang SAMA dengan event non-modul (identity, customization), tabrakan dengan event mereka pun
// ikut tertangkap — pemeriksaan lokal di core/domain tak akan pernah bisa melihatnya.
//
// Idempoten: mendaftar ulang nama dengan tipe yang sama diizinkan registry.
func (r *Registry) RegisterEventSchemas(reg EventSchemaRegistrar) error {
	for _, name := range r.uniqueNames() {
		for _, ev := range r.byName[name].Manifest().Events.Produces {
			if err := reg.Register(ev.Name, ev.Schema); err != nil {
				return fmt.Errorf("modul %q: schema event %q: %w", name, ev.Name, err)
			}
		}
	}
	return nil
}

// ExternalSubscription adalah satu entri Events.Consumes yang menunjuk event yang TIDAK
// diproduksi modul terdaftar mana pun.
type ExternalSubscription struct {
	Module  string // modul yang men-subscribe
	Event   string // nama event yang dikonsumsi
	Handler string // nama handler sesuai manifest
}

// ExternalSubscriptions mengembalikan seluruh entri Consumes yang produsennya tidak ada di
// registry ini, terurut deterministik (urutan registrasi modul, lalu urutan manifest).
//
// Ini SENGAJA bukan error boot. Consumes antar modul memang loose coupling: modul konsumen tidak
// mensyaratkan produsennya ikut dipasang (lihat DependsOn modul referensi), dan deployment pemda
// yang berbeda memasang himpunan modul yang berbeda. Menggagalkan boot di sini akan mengubah
// setiap subscribe menjadi dependency keras — persis yang dihindari desainnya.
//
// Yang perlu diketahui operator justru bentuk kegagalannya: pada jalur NATS, pesan yang tiba
// untuk event tanpa schema terdaftar DIBUANG diam-diam (infra/eventbus/nats.go — Unmarshal butuh
// schema), jadi subscriber-nya tuli tanpa gejala. Selama produsennya memang tidak terpasang tak
// ada yang hilang (tak ada yang menerbitkan event itu), tapi kondisinya harus TERLIHAT saat boot,
// bukan ditemukan saat menunggu event yang tak pernah datang.
//
// Batas yang jujur: registry hanya tahu manifest MODUL. Event yang diproduksi komponen non-modul
// (identity, customization — yang mendaftarkan schema-nya lewat registrar sendiri) tetap muncul
// di sini meski schema-nya terdaftar. Pemanggil melaporkannya sebagai catatan, bukan cacat.
func (r *Registry) ExternalSubscriptions() []ExternalSubscription {
	produced := make(map[string]bool)
	for _, name := range r.uniqueNames() {
		for _, ev := range r.byName[name].Manifest().Events.Produces {
			produced[ev.Name] = true
		}
	}

	var out []ExternalSubscription
	for _, name := range r.uniqueNames() {
		for _, sub := range r.byName[name].Manifest().Events.Consumes {
			if produced[sub.Event] {
				continue
			}
			out = append(out, ExternalSubscription{Module: name, Event: sub.Event, Handler: sub.Handler})
		}
	}
	return out
}
