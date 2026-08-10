package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// authorityGuard menegakkan CONTAINMENT aktor→target pada mutasi identitas (ADR-019,
// REVIEW_BACKLOG B7). Sebelum ini, ketiga mutasi identitas hanya bertanya "apakah aktor punya
// permission?" dan tak pernah "apakah TARGET berada dalam wewenang aktor?" — sehingga scope
// yang disaring saat LOGIN tak pernah diperiksa lagi terhadap sasaran satu operasi.
//
// Aturannya diambil apa adanya dari model Kubernetes RBAC (*Privilege escalation prevention*):
// seseorang hanya boleh MEMBUAT wewenang yang ia sendiri sudah pegang, pada scope yang sama —
// atau ia diberi verb `escalate` secara eksplisit. Terjemahannya di sini:
//
//  1. TENANT — operasi yang menyebut tenant hanya boleh menyebut tenant TOKEN aktor.
//  2. ROLE   — role sentral hanya boleh diberikan bila aktor memegang SELURUH permission
//     yang role itu berikan; role global hanya oleh pemegang pintu keluar.
//  3. TARGET — kredensial hanya boleh diterbitkan untuk person yang wewenangnya tidak
//     melampaui wewenang aktor (menerbitkan kredensial = menjadi target).
//
// Pintu keluarnya satu dan eksplisit: domain.PermAuthorityEscalate.
//
// Aturan 1 & 2 murni fungsi dari konteks aktor, jadi keduanya fungsi bebas — use case yang tak
// butuh membaca role target tak perlu ikut menerima repo. Hanya aturan 3 punya state.
//
// KENAPA "tenant token", bukan klaim tenant_scope. Token Pamong SELALU ter-scope ke tepat satu
// tenant, dan CentralRoleResolver sudah menyaring role sentral untuk tenant itu saat login
// (PR-2.4.3) — klaim `tenant_scope` karena itu sengaja diterbitkan KOSONG (lihat
// scopedTokenMinter.mint). Wewenang teritorial aktor pada satu request karena itu PERSIS
// ctx.TenantID(). Menambahkan TenantScope() ke port.AuthContext hanya untuk membaca daftar yang
// selalu kosong akan memasang kontrak dorman lintas layer — tepat pola yang dibayar Sub-phase
// 5.0. Bila kelak token multi-tenant diterbitkan, yang berubah cukup fungsi ini.
type targetAuthorityGuard struct {
	roles       domain.CentralRoleRepository
	assignments domain.CentralRoleAssignmentRepository
	now         func() time.Time
}

func newTargetAuthorityGuard(
	roles domain.CentralRoleRepository,
	assignments domain.CentralRoleAssignmentRepository,
) targetAuthorityGuard {
	return targetAuthorityGuard{roles: roles, assignments: assignments, now: time.Now}
}

// errEscalation adalah penolakan seragam containment: 403, menyebut permission pintu keluar
// yang KURANG — bukan permission operasinya. Aktor yang sah tetap ditolak dengan alasan yang
// bisa ia tindaklanjuti ("minta escalate"), dan auditor melihat satu kode untuk seluruh keluarga.
func errEscalation() error {
	return core.ErrPermissionDenied(domain.PermAuthorityEscalate)
}

// mayEscalate melaporkan apakah aktor memegang pintu keluar eksplisit.
//
// PERINGATAN yang harus ikut dibaca tiap kali fungsi ini dipanggil: gateway.Context.
// RequirePermission mengembalikan nil bila TIDAK ADA evaluator ter-wire (default permisif
// warisan, lihat gateway/context.go). Konteks tanpa evaluator karena itu terbaca "boleh
// eskalasi" — arah gagal yang berlawanan dengan seluruh isi file ini. Hari ini jalur itu tak
// terjangkau di HTTP (RequireAuth memagari /admin/identity/*, dan evaluatorFactory.Build tak
// pernah mengembalikan nil), dan pada konteks tanpa evaluator gerbang permission di ATAS use
// case pun sudah lolos — jadi ia tak menambah lubang baru. Yang ia tambah adalah
// KETERGANTUNGAN: melonggarkan default permisif itu kelak akan mematikan containment
// bersamaan, diam-diam.
//
// Karena itu requireTenantBound di bawah dipasang di setiap aturan SEBELUM fungsi ini
// dipanggil: ia menuntut sinyal POSITIF (tenant dari klaim tersigning) yang tak bisa datang
// dari default permisif. Akar masalahnya — RequirePermission yang permisif saat evaluator nil —
// dijadwalkan terpisah di ROADMAP; ia perubahan postur repo-wide, bukan tambalan di sini.
func mayEscalate(ctx port.AuthContext) bool {
	return ctx.RequirePermission(domain.PermAuthorityEscalate) == nil
}

// requireTenantBound menolak aktor yang tidak terikat tenant. Ia sengaja mendahului
// mayEscalate di setiap aturan: nilainya berasal dari klaim JWT tersigning lewat
// TenantResolver, bukan dari evaluator, sehingga konteks tanpa evaluator (anonim, job/CLI yang
// belum merakit stack) GAGAL di sini alih-alih lolos lewat pintu keluar.
//
// Tak ada aktor sah yang kehilangan sesuatu: ketiga permission mutasi identitas hanya bisa
// datang dari role sentral (B6), dan role sentral hanya berlaku bagi persona employee — yang
// tokennya selalu ter-scope satu tenant.
func requireTenantBound(ctx port.AuthContext) error {
	if ctx.TenantID() == "" {
		return errEscalation()
	}
	return nil
}

// requireTenantWithinAuthority menegakkan aturan 1: tenantID harus tenant token aktor.
//
// requireTenantBound mendahului mayEscalate di sini persis seperti pada dua aturan lain — kalau
// tidak, aturan ini SENDIRI yang menjadi celah: pada konteks tanpa evaluator, mayEscalate
// mengembalikan true dan gerbang tenant lolos, sementara dua aturan lain tetap fail-closed pada
// konteks yang sama. Invariant yang hanya berlaku di sebagian aturan bukan invariant.
func requireTenantWithinAuthority(ctx port.AuthContext, tenantID string) error {
	if err := requireTenantBound(ctx); err != nil {
		return err
	}
	if tenantID == ctx.TenantID() {
		return nil
	}
	if mayEscalate(ctx) {
		return nil
	}
	return errEscalation()
}

// requireRoleWithinAuthority menegakkan aturan 2 untuk SATU role sentral beserta scope yang
// diminta: aktor wajib memegang setiap permission yang role itu berikan, dan role global —
// yang berlaku di SEMUA tenant sekaligus — selalu di luar wewenang aktor yang ter-scope satu
// tenant.
//
// Kepemilikan permission aktor diperiksa lewat ctx.RequirePermission, yakni lewat Engine +
// katalog yang sama dengan jalur otorisasi biasa. Jadi resolusi konflik penuh (global-precedence,
// strict-intersection) dan pengurungan lapis asal role (ADR-019/B8) ikut berlaku di sini tanpa
// diulang — dan itu penting: kalau pemeriksaan ini punya jalur evaluasinya sendiri, ia akan
// menyimpang dari otorisasi sesungguhnya diam-diam.
func requireRoleWithinAuthority(
	ctx port.AuthContext, role *domain.CentralRole, tenantScope []string,
) error {
	if err := requireTenantBound(ctx); err != nil {
		return err
	}
	if mayEscalate(ctx) {
		return nil
	}
	if role.ScopeType == domain.ScopeGlobal {
		return errEscalation()
	}
	// Scope kosong ("") ikut tertolak di sini tanpa cabang sendiri: requireTenantBound di atas
	// menjamin ctx.TenantID() tak kosong, jadi "" tak akan pernah sama dengannya. Menambahkan
	// `|| s == ""` hanya akan menjadi cabang mati yang menyiratkan pagar yang tak ada.
	for _, s := range tenantScope {
		if s != ctx.TenantID() {
			return errEscalation()
		}
	}
	for _, perm := range role.Permissions {
		if err := ctx.RequirePermission(perm); err != nil {
			return errEscalation()
		}
	}
	return nil
}

// requirePersonWithinAuthority menegakkan aturan 3: seluruh role sentral AKTIF milik target
// harus berada dalam wewenang aktor. Dipakai jalur yang efektif menjadikan aktor sebagai target
// — menerbitkan kredensial.
//
// Penyaringan assignment target SENGAJA longgar di DUA sumbu, karena artefak yang diotorisasi —
// kredensial — berumur permanen sementara setiap penyaringan hanya memotret satu titik:
//
//   - Tenant-agnostik (bukan AppliesTo terhadap tenant aktor). Aktor di tenant X yang menerbitkan
//     kredensial bagi orang ber-role scoped di tenant Y bisa login sebagai orang itu lalu memilih
//     tenant Y (POST /auth/select-tenant) dan memakai wewenangnya di sana.
//   - Waktu-agnostik ke DEPAN (Expired, bukan ActiveAt). valid_from datang dari klien: assignment
//     yang dijadwalkan mulai pekan depan tampak "tidak aktif" hari ini, sehingga kredensialnya
//     terbit sekarang dan wewenangnya dipanen nanti.
//
// Hanya ValidUntil yang sudah lewat yang aman diabaikan. Scope target tetap diperiksa di
// requireRoleWithinAuthority, yang menolak scope apa pun di luar tenant aktor.
//
// Batas yang diketahui & disengaja: yang diperiksa adalah wewenang SENTRAL target. Role TENANT
// target hidup di DB tenant, sedangkan use case identity hanya berbicara ke identity DB; memeriksanya
// menuntut port lintas-DB baru. Sisa risikonya lateral DI DALAM satu tenant (menerbitkan kredensial
// untuk sesama pegawai yang role tenant-nya lebih luas), bukan pengambilalihan platform yang jadi
// inti B7(a) — dan pemegang identity:credential:buat sendiri selalu principal yang ditunjuk admin
// platform (B6). Dicatat sebagai residu di REVIEW_BACKLOG, bukan diam-diam dianggap tertutup.
func (g targetAuthorityGuard) requirePersonWithinAuthority(ctx port.AuthContext, personID uuid.UUID) error {
	if err := requireTenantBound(ctx); err != nil {
		return err
	}
	if mayEscalate(ctx) {
		return nil
	}
	assigns, err := g.assignments.ListByPerson(ctx, personID)
	if err != nil {
		return err
	}
	now := g.now()
	for _, a := range assigns {
		if a.Expired(now) {
			continue // masa berlakunya sudah lewat & tak akan kembali — satu-satunya yang aman diabaikan
		}
		role, err := g.roles.FindByID(ctx, a.RoleID)
		if err != nil {
			return err
		}
		if err := requireRoleWithinAuthority(ctx, role, a.TenantScope); err != nil {
			return err
		}
	}
	return nil
}
