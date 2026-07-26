package workflow

import "context"

// Notifikasi transisi (PRD F3, PR-N2). Seam OPSIONAL — sama seperti DeadlineScheduler
// (sla.go): nil = notifikasi transisi nonaktif, engine berperilaku persis seperti sebelum
// PR-N2. Semua ketergantungan ke luar (core/notification) lewat PORT ini — engine tidak
// pernah menyentuh core/notification secara konkret. Engine tetap TENANT-AGNOSTIK: NotifySpec
// yang dipanggilkan ke sini sudah memakai peran KONKRET tenant (RoleBindings diterapkan oleh
// Engine sebelum memanggil — lihat ExecuteWithComment), bukan peran generik dari definisi.

// TransitionNotifier mengirim notifikasi transisi (NotifySpec) setelah satu transisi SUKSES.
// Diimplementasikan di luar core (adapter atas core/notification.RoleNotifier): resolusi
// peran->orang (termasuk fallback PLT) + render template + kirim channel terjadi DI SANA.
// TransitionNotifier TIDAK menjalankan business logic atau memutasi data — murni notifikasi.
//
// Kegagalan NotifyTransition TIDAK membatalkan transisi domain: pada titik pemanggilan,
// transisi sudah OTORITATIF (state sudah berpindah, history sudah tercatat, SLA state baru
// sudah terjadwal). ExecuteWithComment tetap MENGEMBALIKAN error notifikasi ke caller (bukan
// menelannya) — best-effort dengan jejak error tetap terlihat, sehingga caller asinkron
// (outbox/relay) bisa memutuskan retry tanpa harus menafsirkannya sebagai "transisi gagal".
// instance yang diteruskan sudah mencerminkan state SETELAH transisi.
type TransitionNotifier interface {
	NotifyTransition(ctx context.Context, tenantID string, spec NotifySpec, inst WorkflowInstance) error
}
