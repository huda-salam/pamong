// Package notification adalah Notification Hub framework: mengirim notifikasi lintas channel
// (in-app, email, push) dengan template yang bisa dikustomisasi per-tenant + i18n, dan
// melacak status pengiriman. Channel didaftarkan ke registry (titik ekstensi #1) sehingga
// menambah channel baru = daftar satu baris, pemanggil tak berubah (CLAUDE.md §Fleksibilitas).
//
// Batas tanggung jawab (PRD): hub MENYUSUN konten (template) & memilih channel; pengiriman
// FISIK ada di infra/messaging (lewat port.MessagingPort). Resolusi peran→orang (routing by
// role + fallback PLT) hidup di Router (routing.go): KEBIJAKAN fallback ada di core, SUMBER
// datanya pluggable lewat RecipientDirectory. Hub sendiri hanya tahu penerima konkret.
//
// PR-3.6.1: channel abstraction (F1) + template engine per-tenant/i18n (F2) + delivery
// tracking dasar (F4).
// PR-3.6.2: routing by role/jabatan + fallback PLT (F3) — Router/RoleNotifier + RecipientDirectory.
//
// PR-N1: adapter tenant-DB untuk RecipientDirectory (infra/notification.DBRecipientDirectory) —
// jalur in-app jalan penuh lewat gov.tenant_roles/gov.user_role_assignments nyata. ActingFor
// (PLT-jabatan) DEFERRED(Phase-7.x — modul kepegawaian): lihat doc DBRecipientDirectory.ActingFor.
//
// PR-N3b (ADR-013): channel email/SMS kini mendapat alamat kontak. Kontak (email/no_hp) di-clone
// dari id.persons ke gov.user_profiles tenant lewat event identity.employment.ditugaskan, lalu
// DBRecipientDirectory.fillContacts mengisi Recipient.Email/Phone best-effort. Kontak yang tak
// tersedia (belum ter-clone / NULL) tetap kosong → channel bersangkutan gagal anggun.
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
	// TemplateKey adalah kunci template yang menghasilkan pesan ini. Dibawa agar channel yang
	// MENYIMPAN notifikasi (in-app) bisa mencatat asal-usulnya — tanpa ini kolom
	// gov.notification_inapp.template_key tak pernah terisi di jalur produksi mana pun, dan UI
	// tak punya cara mengelompokkan atau menyaring inbox per jenis kejadian.
	TemplateKey string
}
