// Package user menyediakan driven adapter Postgres untuk port.UserResolver: baca READ-ONLY
// clone identitas (gov.user_profiles) pada TENANT DB. Modul bisnis mengakses data user HANYA
// lewat port.UserResolver — bukan query gov.user_profiles langsung, bukan lintas-DB ke identity
// (CLAUDE.md §Identity). Clone diisi identity/sync via event; adapter ini tidak menulisnya.
package user

import (
	"context"
	stdsync "sync"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// Purpose kolom terenkripsi pada clone — nilainya wajib sama dengan yang dipakai penulis
// (identity/sync), kalau tidak blind index tak pernah cocok dan blob ditolak pemeriksaan
// purpose (ADR-015).
const (
	purposeNIK = "nik"
	purposeNIP = "nip"
)

// DBResolver mengimplementasi port.UserResolver di atas tenant DB (tabel gov.user_profiles).
// Pool tenant diresolusi per-request dari TenantConnManager: tenant diambil dari context
// (port.TenantFrom, disuntik middleware tenant resolver) — DB-per-tenant, tenant-scoped.
//
// Adapter ini murni PEMBACA: ia tidak meng-ensure skema (itu tanggung jawab identity/sync saat
// menulis clone). Bila gov.user_profiles belum ada untuk tenant (sync belum jalan), query gagal
// lantang — sinyal misconfigurasi onboarding, bukan disembunyikan.
//
// NIK & NIP pada clone TERENKRIPSI (PR-3.8.5): dibaca dari {f}_enc lalu dibuka, dan dicari
// lewat {f}_bidx. Realm kunci = tenant, sama dengan yang dipakai penulis.
type DBResolver struct {
	connMgr *db.TenantConnManager
	crypto  port.CryptoPort
	sealers stdsync.Map // tenantID -> *crypto.FieldSealer
}

// NewDBResolver membuat resolver di atas manajer koneksi tenant. CryptoPort WAJIB: tanpa itu
// kolom pengenal clone tak bisa dibuka sama sekali — dan menerima nil hanya akan memindahkan
// kegagalannya ke query pertama, dalam bentuk yang tak menyebut sebabnya.
func NewDBResolver(connMgr *db.TenantConnManager, c port.CryptoPort) (*DBResolver, error) {
	if c == nil {
		return nil, core.ErrValidation("crypto", "UserResolver butuh port.CryptoPort (clone berpengenal terenkripsi, ADR-009)")
	}
	return &DBResolver{connMgr: connMgr, crypto: c}, nil
}

var _ port.UserResolver = (*DBResolver)(nil)

// profileCols — kolom yang dipetakan ke port.UserProfile. Kontak (email/no_hp), assignment_id,
// synced_at, unit_kerja_id sengaja TIDAK diambil: bukan bagian kontrak UserResolver.
// _bidx juga tidak dibaca: ia alat pencarian, bukan sumber nilai.
const profileCols = `id, nik_enc, nip_enc, nama_lengkap, employment_status, is_cross_tenant, jabatan_lokal`

// ResolveByID mencari profil user berdasarkan person_id (= id pada clone).
func (r *DBResolver) ResolveByID(ctx context.Context, id uuid.UUID) (*port.UserProfile, error) {
	// gov:raw-ok reason=read-user-clone query=user-profile-by-id
	return r.one(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE id = $1`, id.String(), id)
}

// ResolveByNIP mencari profil user berdasarkan NIP (hanya person ber-employment ASN) lewat
// blind index — nip_enc tak bisa dipakai equality (nonce acak membuat NIP yang sama
// menghasilkan ciphertext berbeda tiap penulisan).
func (r *DBResolver) ResolveByNIP(ctx context.Context, nip string) (*port.UserProfile, error) {
	bidx, err := r.index(ctx, purposeNIP, nip)
	if err != nil {
		return nil, err
	}
	// Referensi error menyebut JENIS pencarian, bukan NIP-nya: pesan FrameworkError mengalir ke
	// log DAN body HTTP — jalur samping yang sama (ADR-009 §6). Pemanggil sudah tahu nilai yang
	// ia kirim, jadi tak ada informasi yang hilang.
	// gov:raw-ok reason=read-user-clone query=user-profile-by-nip
	return r.one(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE nip_bidx = $1`, purposeNIP, bidx)
}

// ResolveByNIK mencari profil user berdasarkan NIK (anchor identitas) lewat blind index.
func (r *DBResolver) ResolveByNIK(ctx context.Context, nik string) (*port.UserProfile, error) {
	bidx, err := r.index(ctx, purposeNIK, nik)
	if err != nil {
		return nil, err
	}
	// gov:raw-ok reason=read-user-clone query=user-profile-by-nik
	return r.one(ctx, `SELECT `+profileCols+` FROM gov.user_profiles WHERE nik_bidx = $1`, purposeNIK, bidx)
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

// sealer mengembalikan sealer ber-realm tenant aktif. Realm = tenant, sama dengan penulis
// clone (identity/sync): salah realm tidak gagal, ia hanya membuat bidx tak pernah cocok.
func (r *DBResolver) sealer(tenantID string) (*crypto.FieldSealer, error) {
	if s, ok := r.sealers.Load(tenantID); ok {
		return s.(*crypto.FieldSealer), nil
	}
	s, err := crypto.NewFieldSealer(r.crypto, tenantID, "infra/user")
	if err != nil {
		return nil, err
	}
	actual, _ := r.sealers.LoadOrStore(tenantID, s)
	return actual.(*crypto.FieldSealer), nil
}

// index menghitung blind index pencarian pada realm tenant aktif.
func (r *DBResolver) index(ctx context.Context, purpose, plain string) ([]byte, error) {
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	s, err := r.sealer(tenantID)
	if err != nil {
		return nil, err
	}
	return s.Index(ctx, purpose, plain)
}

// one menjalankan query satu-baris lalu memetakan & membuka kolom terenkripsinya.
func (r *DBResolver) one(ctx context.Context, sql, ref string, arg any) (*port.UserProfile, error) {
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	pool, err := r.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	s, err := r.sealer(tenantID)
	if err != nil {
		return nil, err
	}
	return scanProfile(ctx, pool.QueryRow(ctx, sql, arg), s, ref)
}

// tenantOf mengambil tenant aktif dari context. Jalur employee WAJIB ber-tenant (disuntik
// middleware); kosong = bug wiring, bukan "user tak ditemukan" — tolak eksplisit.
func tenantOf(ctx context.Context) (string, error) {
	tenantID := port.TenantFrom(ctx)
	if tenantID == "" {
		return "", core.ErrValidation("tenant", "tenant tidak ada di context saat resolve user")
	}
	return tenantID, nil
}

// scanProfile memetakan satu baris gov.user_profiles ke port.UserProfile lalu membuka NIK &
// NIP. NIP nullable (non-ASN) → kosong; jabatan_lokal nullable (belum diisi modul kepegawaian).
//
// Identitas baris untuk AAD (ADR-016) diambil dari BARIS ITU SENDIRI (p.ID hasil scan), tak
// pernah dari argumen pencarian — sehingga baris yang ciphertext-nya dipindahkan lewat SQL
// gagal dibuka di sini.
func scanProfile(ctx context.Context, row port.Row, s *crypto.FieldSealer, ref string) (*port.UserProfile, error) {
	var (
		p              port.UserProfile
		nikEnc, nipEnc []byte
		jabatan        *string
	)
	if err := row.Scan(&p.ID, &nikEnc, &nipEnc, &p.NamaLengkap, &p.EmploymentStatus, &p.IsCrossTenant, &jabatan); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("UserProfile", ref)
		}
		return nil, err
	}
	var err error
	if p.NIK, err = s.Open(ctx, purposeNIK, p.ID, nikEnc); err != nil {
		return nil, err
	}
	if p.NIP, err = s.Open(ctx, purposeNIP, p.ID, nipEnc); err != nil {
		return nil, err
	}
	if jabatan != nil {
		p.JabatanLokal = *jabatan
	}
	return &p, nil
}
