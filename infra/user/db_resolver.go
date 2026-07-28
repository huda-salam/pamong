// Package user menyediakan driven adapter Postgres untuk port.UserResolver: baca READ-ONLY
// clone identitas (gov.user_profiles) pada TENANT DB. Modul bisnis mengakses data user HANYA
// lewat port.UserResolver — bukan query gov.user_profiles langsung, bukan lintas-DB ke identity
// (CLAUDE.md §Identity). Clone diisi identity/sync via event; adapter ini tidak menulisnya.
package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// DBResolver mengimplementasi port.UserResolver di atas tenant DB (tabel gov.user_profiles).
// Pool tenant diresolusi per-request dari TenantConnManager: tenant diambil dari context
// (port.TenantFrom, disuntik middleware tenant resolver) — DB-per-tenant, tenant-scoped.
//
// Adapter ini murni PEMBACA: ia tidak meng-ensure skema (itu tanggung jawab identity/sync saat
// menulis clone). Bila gov.user_profiles belum ada untuk tenant (sync belum jalan), query gagal
// lantang — sinyal misconfigurasi onboarding, bukan disembunyikan.
type DBResolver struct {
	connMgr *db.TenantConnManager
}

// NewDBResolver membuat resolver di atas manajer koneksi tenant.
func NewDBResolver(connMgr *db.TenantConnManager) *DBResolver {
	return &DBResolver{connMgr: connMgr}
}

var _ port.UserResolver = (*DBResolver)(nil)

// profileCols — kolom yang dipetakan ke port.UserProfile. Kontak (email/no_hp), assignment_id,
// synced_at, unit_kerja_id sengaja TIDAK diambil: bukan bagian kontrak UserResolver.
const profileCols = `id, nik, nip, nama_lengkap, employment_status, is_cross_tenant, jabatan_lokal`

// ResolveByID mencari profil user berdasarkan person_id (= id pada clone).
func (r *DBResolver) ResolveByID(ctx context.Context, id uuid.UUID) (*port.UserProfile, error) {
	// gov:raw-ok reason=read-user-clone query=user-profile-by-id
	row, err := r.queryRow(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return scanProfile(row, id.String())
}

// ResolveByNIP mencari profil user berdasarkan NIP (hanya person ber-employment ASN).
func (r *DBResolver) ResolveByNIP(ctx context.Context, nip string) (*port.UserProfile, error) {
	// gov:raw-ok reason=read-user-clone query=user-profile-by-nip
	row, err := r.queryRow(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE nip = $1`, nip)
	if err != nil {
		return nil, err
	}
	return scanProfile(row, nip)
}

// ResolveByNIK mencari profil user berdasarkan NIK (anchor identitas).
func (r *DBResolver) ResolveByNIK(ctx context.Context, nik string) (*port.UserProfile, error) {
	// gov:raw-ok reason=read-user-clone query=user-profile-by-nik
	row, err := r.queryRow(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE nik = $1`, nik)
	if err != nil {
		return nil, err
	}
	return scanProfile(row, nik)
}

// IsCrossTenant melaporkan apakah user ini ditugaskan lintas-tenant (mis. PJ/Plt dari daerah
// lain) di tenant aktif.
func (r *DBResolver) IsCrossTenant(ctx context.Context, id uuid.UUID) (bool, error) {
	prof, err := r.ResolveByID(ctx, id)
	if err != nil {
		return false, err
	}
	return prof.IsCrossTenant, nil
}

// HasCentralRole — DEFERRED(Phase-5.x): central role hidup di IDENTITY DB (id.central_role_*),
// bukan pada clone tenant gov.user_profiles yang dibaca adapter ini. Menjawabnya butuh lookup ke
// identity DB (dependency terpisah). Sampai jalur itu di-wire, GAGAL LANTANG (501) alih-alih
// mengembalikan false yang diam-diam salah — tak ada pemanggil pada jalur live saat ini.
func (r *DBResolver) HasCentralRole(ctx context.Context, id uuid.UUID, role string) (bool, error) {
	return false, core.ErrUnimplemented("UserResolver.HasCentralRole")
}

// queryRow meresolusi pool tenant dari context lalu menjalankan query satu-baris.
func (r *DBResolver) queryRow(ctx context.Context, sql string, args ...any) (port.Row, error) {
	tenantID := port.TenantFrom(ctx)
	if tenantID == "" {
		// Jalur employee WAJIB ber-tenant (disuntik middleware). Kosong = bug wiring, bukan
		// "user tak ditemukan" — tolak eksplisit.
		return nil, core.ErrValidation("tenant", "tenant tidak ada di context saat resolve user")
	}
	pool, err := r.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return pool.QueryRow(ctx, sql, args...), nil
}

// scanProfile memetakan satu baris gov.user_profiles ke port.UserProfile. NIP & jabatan_lokal
// nullable (non-ASN / belum diisi modul kepegawaian) → di-scan ke *string lalu dinormalkan ke "".
func scanProfile(row port.Row, ref string) (*port.UserProfile, error) {
	var (
		p            port.UserProfile
		nip, jabatan *string
	)
	if err := row.Scan(&p.ID, &p.NIK, &nip, &p.NamaLengkap, &p.EmploymentStatus, &p.IsCrossTenant, &jabatan); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("UserProfile", ref)
		}
		return nil, err
	}
	if nip != nil {
		p.NIP = *nip
	}
	if jabatan != nil {
		p.JabatanLokal = *jabatan
	}
	return &p, nil
}
