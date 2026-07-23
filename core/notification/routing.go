package notification

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// RoleTarget mendeskripsikan SASARAN notifikasi sebagai peran/jabatan, bukan orang konkret —
// inti F3: notifikasi ditujukan ke "Kadis", bukan ke person_id tertentu, sehingga tetap sampai
// ke pejabat yang tepat meski orangnya berganti (mutasi) atau sedang digantikan (PLT).
//
// Role adalah nama role tenant (mis. "kepala_dinas") — bukan permission. UnitKerjaID mempersempit
// ke satu unit kerja (mis. "Kadis Dukcapil" vs "Kadis Kesehatan"); nil = tenant-wide.
type RoleTarget struct {
	TenantID    string
	Role        string
	UnitKerjaID *uuid.UUID // nil = tanpa batas unit kerja
}

// key mengembalikan kunci kanonik (tenant|role|unit) untuk mencocokkan target di direktori
// in-memory. Deterministik; unit nil → segmen kosong.
func (t RoleTarget) key() string {
	unit := ""
	if t.UnitKerjaID != nil {
		unit = t.UnitKerjaID.String()
	}
	return strings.Join([]string{t.TenantID, t.Role, unit}, "|")
}

// Router meresolusi RoleTarget → penerima konkret dengan KEBIJAKAN fallback PLT: pejabat
// definitif (HoldersOf) diutamakan; hanya bila jabatan kosong, jatuh ke pelaksana/PLT
// (ActingFor). Kebijakan urutan ini hidup di core (teruji, deterministik); sumber datanya
// pluggable lewat RecipientDirectory. Router tak menyentuh DB — persis pola config.Resolver.
type Router struct {
	dir RecipientDirectory
}

// NewRouter merakit router di atas sebuah direktori.
func NewRouter(dir RecipientDirectory) *Router {
	return &Router{dir: dir}
}

// Resolve mengembalikan penerima untuk target. Urutan: (1) pejabat definitif; (2) bila kosong,
// PLT/pelaksana. ErrNoRecipient bila keduanya kosong — notifikasi ke peran tak bertuan adalah
// kesalahan konfigurasi yang harus terlihat, bukan hilang diam-diam.
func (r *Router) Resolve(ctx context.Context, t RoleTarget) ([]Recipient, error) {
	holders, err := r.dir.HoldersOf(ctx, t)
	if err != nil {
		return nil, err
	}
	if len(holders) > 0 {
		return holders, nil
	}
	// Jabatan kosong → fallback ke PLT (delegasi core/permission).
	acting, err := r.dir.ActingFor(ctx, t)
	if err != nil {
		return nil, err
	}
	if len(acting) == 0 {
		return nil, ErrNoRecipient(t.TenantID, t.Role)
	}
	return acting, nil
}

// RoleNotifier menyatukan routing (peran→orang) dengan Hub (render+kirim+catat). Inilah
// entry point yang dipakai pemanggil (workflow/eskalasi SLA): "kirim template X ke peran Y".
// Resolusi peran ke penerima konkret — termasuk fallback PLT — terjadi DI SINI, di depan Hub;
// Hub sendiri tetap hanya tahu penerima konkret (pemisahan tanggung jawab PRD).
type RoleNotifier struct {
	router *Router
	hub    *Hub
}

// NewRoleNotifier merakit notifier dari router dan hub.
func NewRoleNotifier(router *Router, hub *Hub) *RoleNotifier {
	return &RoleNotifier{router: router, hub: hub}
}

// NotifyRole meresolusi target ke penerima lalu mengirim notifikasi ke SETIAP penerima lewat
// channel yang diminta. Mengembalikan jumlah penerima yang disasar dan gabungan error
// pengiriman (per penerima) — sebagian gagal tak menghentikan sisanya, konsisten dengan Hub.Send.
// Error resolusi (mis. peran tak bertuan) dikembalikan langsung dengan count 0.
func (n *RoleNotifier) NotifyRole(ctx context.Context, t RoleTarget, templateKey string, data map[string]any, channels ...string) (int, error) {
	recipients, err := n.router.Resolve(ctx, t)
	if err != nil {
		return 0, err
	}
	var sendErrs []error
	for _, r := range recipients {
		sendErr := n.hub.Send(ctx, Notification{
			TenantID:    t.TenantID,
			Recipient:   r,
			TemplateKey: templateKey,
			Data:        data,
			Channels:    channels,
		})
		if sendErr != nil {
			sendErrs = append(sendErrs, sendErr)
		}
	}
	return len(recipients), errors.Join(sendErrs...)
}

// MemoryDirectory adalah RecipientDirectory in-memory untuk seed/test. Pemegang jabatan dan
// pelaksana (PLT) diset terpisah per target sehingga skenario "jabatan kosong → PLT" bisa
// diperagakan tanpa DB. Adapter tenant-DB (baca gov.user_role_assignments + gov.delegations)
// menyusul saat seam kontak email/telepon tersedia — lihat catatan di paket doc.
type MemoryDirectory struct {
	holders map[string][]Recipient
	acting  map[string][]Recipient
}

// NewMemoryDirectory membuat direktori kosong.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{
		holders: make(map[string][]Recipient),
		acting:  make(map[string][]Recipient),
	}
}

var _ RecipientDirectory = (*MemoryDirectory)(nil)

// SetHolders menetapkan pemegang definitif untuk target.
func (d *MemoryDirectory) SetHolders(t RoleTarget, rs ...Recipient) {
	d.holders[t.key()] = rs
}

// SetActing menetapkan pelaksana/PLT untuk target (dipakai saat pemegang kosong).
func (d *MemoryDirectory) SetActing(t RoleTarget, rs ...Recipient) {
	d.acting[t.key()] = rs
}

// HoldersOf mengembalikan pemegang definitif target.
func (d *MemoryDirectory) HoldersOf(_ context.Context, t RoleTarget) ([]Recipient, error) {
	return d.holders[t.key()], nil
}

// ActingFor mengembalikan pelaksana/PLT target.
func (d *MemoryDirectory) ActingFor(_ context.Context, t RoleTarget) ([]Recipient, error) {
	return d.acting[t.key()], nil
}
