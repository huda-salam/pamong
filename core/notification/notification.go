// Package notification adalah Notification Hub framework: mengirim notifikasi lintas channel
// (in-app, email, push) dengan template yang bisa dikustomisasi per-tenant + i18n, dan
// melacak status pengiriman. Channel didaftarkan ke registry (titik ekstensi #1) sehingga
// menambah channel baru = daftar satu baris, pemanggil tak berubah (CLAUDE.md §Fleksibilitas).
//
// Batas tanggung jawab (PRD): hub MENYUSUN konten (template) & memilih channel; pengiriman
// FISIK ada di infra/messaging (lewat port.MessagingPort). Resolusi peran→orang (routing by
// role + fallback PLT) BUKAN di sini — itu PR-3.6.2 (core/permission yang me-resolve). Di
// PR-3.6.1 penerima sudah konkret (Recipient hasil resolusi caller).
//
// PR-3.6.1: channel abstraction (F1) + template engine per-tenant/i18n (F2) + delivery
// tracking dasar (F4). Routing by role/jabatan (F3) DEFERRED(PR-3.6.2).
package notification

import "github.com/google/uuid"

// DefaultLocale dipakai bila penerima tak menyatakan locale atau template locale-spesifik
// tak tersedia. Bahasa Indonesia adalah baseline nasional (PRD F2).
const DefaultLocale = "id"

// Recipient adalah penerima notifikasi yang SUDAH konkret — hasil resolusi peran→orang oleh
// caller. Di PR-3.6.2 resolusi ini (termasuk fallback PLT) dilakukan sebelum memanggil Hub;
// hub tidak pernah menyimpan person_id hardcoded sebagai tujuan (anti-pattern PRD).
//
// Email/Phone kosong menandakan kanal itu tak bisa dipakai untuk penerima ini — Hub akan
// mencatat kegagalan channel bersangkutan alih-alih menebak alamat.
type Recipient struct {
	PersonID uuid.UUID // untuk in-app inbox & pelacakan; bukan tujuan transport eksternal
	Email    string    // tujuan channel email; kosong = tak tersedia
	Phone    string    // tujuan channel SMS; kosong = tak tersedia
	Locale   string    // preferensi bahasa; kosong → DefaultLocale
}

// LocaleOrDefault mengembalikan locale penerima atau DefaultLocale bila kosong.
func (r Recipient) LocaleOrDefault() string {
	if r.Locale == "" {
		return DefaultLocale
	}
	return r.Locale
}

// Notification adalah permintaan kirim satu notifikasi ke satu penerima lewat satu/lebih
// channel. Konten TIDAK dirakit caller — caller memberi TemplateKey + Data, Hub yang me-render
// template per-tenant (memisahkan "apa yang dikirim" dari "bagaimana kalimatnya", agar tenant
// bisa mengubah kalimat tanpa menyentuh kode pemanggil).
type Notification struct {
	TenantID    string         // scope template & pelacakan; "" = level platform
	Recipient   Recipient      // penerima konkret
	TemplateKey string         // kunci template ({modul}.{kejadian}), di-resolve per-tenant+locale
	Data        map[string]any // nilai substitusi template
	Channels    []string       // channel tujuan (nama ter-registry); kosong = tolak (ErrNoChannel)
}

// RenderedMessage adalah hasil render template: subjek + body siap kirim untuk satu locale.
// Subject dipakai email (& judul in-app); body adalah isi. Channel yang tak butuh subjek
// (mis. SMS) mengabaikannya.
type RenderedMessage struct {
	Subject string
	Body    string
}
