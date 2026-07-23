package notification

import "context"

// TemplateStore menyimpan template notifikasi ber-tenant + locale. Didefinisikan di domain,
// diimplementasi di infra/notification (Postgres) & di sini (memory, untuk seed/test).
//
// Candidates mengembalikan SEMUA template yang cocok untuk (tenant, key) — baik yang
// tenant-spesifik maupun global (TenantID=""), lintas locale. Pemilihan "paling cocok"
// (tenant-spesifik > global, locale sama > default) dilakukan di TemplateEngine (pure,
// deterministik) — pola yang sama dengan config.Resolver (kandidat dari store, keputusan
// di core). Store tak perlu tahu aturan pemilihan.
type TemplateStore interface {
	Candidates(ctx context.Context, tenantID, key string) ([]Template, error)
	// Upsert menyimpan/menimpa satu template untuk (tenant, key, locale).
	Upsert(ctx context.Context, t Template) error
}

// InAppInbox menyimpan notifikasi in-app per penerima. Channel in-app menulis ke sini;
// UI membaca dari sini (lewat use case, bukan langsung). Diimplementasi di infra (Postgres)
// & memory (test).
type InAppInbox interface {
	// Append menambah satu item ke kotak masuk penerima, mengembalikan ID item.
	Append(ctx context.Context, item InAppItem) (string, error)
	// List mengembalikan item untuk penerima (terbaru dulu), maksimal limit (0 = semua).
	List(ctx context.Context, tenantID string, personID string, limit int) ([]InAppItem, error)
}

// DeliveryRecorder mencatat status pengiriman tiap upaya kirim (F4 delivery tracking).
// Setiap channel yang dicoba menghasilkan satu DeliveryRecord — sukses maupun gagal —
// agar seluruh percobaan dapat diaudit. Diimplementasi di infra (Postgres) & memory (test).
type DeliveryRecorder interface {
	Record(ctx context.Context, rec DeliveryRecord) error
}

// RecipientDirectory me-resolusi peran/jabatan → penerima konkret (F3 routing). Dipisah jadi
// dua jalur agar KEBIJAKAN fallback ("jabatan kosong → PLT") tetap di core (Router), sementara
// SUMBER data (siapa memegang role, siapa pelaksana) pluggable lewat adapter:
//
//   - HoldersOf  → penerima yang AKTIF memegang role pada scope target (jalur utama).
//   - ActingFor  → penerima PLT/pelaksana (delegasi core/permission) — dipakai HANYA saat
//     HoldersOf kosong. Memisahkannya (bukan menggabung di satu query) membuat urutan
//     preferensi eksplisit & teruji, dan mencegah PLT ikut terkirim saat pejabat definitif ada.
//
// Notify berbasis PERAN, bukan person_id hardcoded, agar tak rusak saat mutasi/PLT (PRD F3).
type RecipientDirectory interface {
	HoldersOf(ctx context.Context, t RoleTarget) ([]Recipient, error)
	ActingFor(ctx context.Context, t RoleTarget) ([]Recipient, error)
}
