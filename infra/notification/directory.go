package notification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/infra/db"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
)

// hierarchy adalah subset kecil dari permission.Hierarchy yang dibutuhkan DBRecipientDirectory
// untuk menguji keanggotaan subtree unit kerja. Didefinisikan lokal (bukan import core/permission)
// agar infra/notification tak perlu bergantung ke package permission hanya demi satu method;
// *tenantroledb.OrgUnitHierarchy memenuhi interface ini secara struktural tanpa perlu diketahui
// eksplisit di sini.
type hierarchy interface {
	IsWithin(ctx context.Context, root, unit uuid.UUID) (bool, error)
}

// DBRecipientDirectory mengimplementasi coreNotif.RecipientDirectory di atas Postgres tenant DB:
// membaca pemegang role tenant NYATA (gov.tenant_roles + gov.user_role_assignments), pengganti
// MemoryDirectory di produksi. In-app cukup PersonID; Email/Phone diisi best-effort dari clone
// tenant gov.user_profiles (kontak seam, PR-N3b/ADR-013 — lihat fillContacts).
//
// READER MURNI: tidak membuat/ensure tabel gov.tenant_roles, gov.user_role_assignments, atau
// gov.delegations — tabel itu milik tenantrole/ & delegation/ (ensure-on-write oleh pemiliknya
// sendiri, precedent gov.user_profiles). Isolasi tenant STRUKTURAL: query tak menyebut tenant_id
// karena pool sudah terkoneksi ke tenant DB spesifik (konvensi tenantrole/CLAUDE.md).
type DBRecipientDirectory struct {
	pool *db.Pool
	hier hierarchy
}

// NewDBRecipientDirectory merakit direktori dari pool tenant DB. Hierarki subtree dibangun di
// atas pool yang sama (gov.org_units via tenantrole/adapter/db.OrgUnitHierarchy).
func NewDBRecipientDirectory(pool *db.Pool) *DBRecipientDirectory {
	return &DBRecipientDirectory{pool: pool, hier: tenantroledb.NewOrgUnitHierarchy(pool)}
}

var _ coreNotif.RecipientDirectory = (*DBRecipientDirectory)(nil)

type roleAssignmentCandidate struct {
	userID         uuid.UUID
	unitKerjaID    *uuid.UUID
	includeSubtree bool
}

// HoldersOf mengembalikan pemegang definitif role tenant untuk target (RoleTarget.Role =
// gov.tenant_roles.name). Assignment kedaluwarsa diabaikan. is_cross_tenant SENGAJA TIDAK
// difilter: PJ/Plt luar-daerah yang di-clone + diberi role tenant tetap pemegang definitif
// (caveat cross-tenant di doc coreNotif.RecipientDirectory) — HARUS jatuh di sini, bukan di
// ActingFor.
//
// Scope unit kerja disaring di Go (bukan SQL rekursif ganda): kandidat diambil TANPA filter
// unit, lalu tiap kandidat diuji lewat withinScope (yang memanggil hier.IsWithin hanya bila
// perlu memeriksa subtree).
func (d *DBRecipientDirectory) HoldersOf(ctx context.Context, t coreNotif.RoleTarget) ([]coreNotif.Recipient, error) {
	// gov:raw-ok reason=notif-role-holders query=notification-holders-of
	rows, err := d.pool.Query(ctx, `
		SELECT ura.user_id, ura.unit_kerja_id, ura.include_subtree
		FROM gov.user_role_assignments ura
		JOIN gov.tenant_roles tr ON tr.id = ura.role_id
		WHERE tr.name = $1
		  AND ura.valid_from <= now()
		  AND (ura.valid_until IS NULL OR ura.valid_until > now())`,
		t.Role)
	if err != nil {
		return nil, fmt.Errorf("query pemegang role %q: %w", t.Role, err)
	}
	defer rows.Close()

	var candidates []roleAssignmentCandidate
	for rows.Next() {
		var c roleAssignmentCandidate
		if err := rows.Scan(&c.userID, &c.unitKerjaID, &c.includeSubtree); err != nil {
			return nil, fmt.Errorf("scan pemegang role %q: %w", t.Role, err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi pemegang role %q: %w", t.Role, err)
	}

	// dedup by user_id: satu user bisa punya lebih dari satu assignment aktif untuk role yang
	// sama (mis. di-assign ulang dengan scope unit berbeda) — tanpa ini RoleNotifier mengirim
	// notifikasi dobel ke orang yang sama.
	seen := make(map[uuid.UUID]bool, len(candidates))
	var out []coreNotif.Recipient
	for _, c := range candidates {
		if seen[c.userID] {
			continue
		}
		if t.UnitKerjaID != nil {
			within, err := d.withinScope(ctx, *t.UnitKerjaID, c.unitKerjaID, c.includeSubtree)
			if err != nil {
				return nil, err
			}
			if !within {
				continue
			}
		}
		seen[c.userID] = true
		out = append(out, coreNotif.Recipient{PersonID: c.userID})
	}
	// Kontak bersifat BEST-EFFORT: kegagalannya TIDAK boleh menggagalkan resolusi pemegang —
	// jalur in-app (cukup PersonID) harus tetap jalan meski clone/kontak bermasalah. Error
	// dicatat (bukan ditelan diam-diam) agar tetap terlihat; Recipient.Email/Phone dibiarkan kosong.
	if err := d.fillContacts(ctx, out); err != nil {
		slog.WarnContext(ctx, "notifikasi: gagal mengisi kontak dari gov.user_profiles; kontak dikosongkan (best-effort)",
			"role", t.Role, "error", err)
	}
	return out, nil
}

// fillContacts mengisi Email/Phone tiap Recipient dari clone tenant gov.user_profiles
// (PR-N3b, ADR-013) secara BEST-EFFORT:
//   - Kolom kontak belum tentu ada: (a) tabel bisa absen karena pemegang role BISA eksis tanpa
//     profil (gov.user_role_assignments sengaja tanpa FK ke gov.user_profiles, tenantrole/
//     schema.go); (b) selama window rollout, tabel bisa ada tapi dibuat versi lama TANPA kolom
//     email/no_hp (writer menambahnya lewat ALTER pada write berikutnya, tapi reader tak menulis).
//     Karena itu keberadaan KEDUA kolom diperiksa dulu (information_schema) — bila tak lengkap,
//     kontak dibiarkan kosong. Kegagalan DB apa pun di sini juga best-effort: caller (HoldersOf)
//     mencatat error dan tetap mengembalikan pemegang, sehingga jalur in-app TIDAK ikut gagal.
//   - Person tanpa baris profil, atau kontak NULL, dibiarkan kosong. Recipient.Email/Phone
//     kosong = kanal itu tak tersedia → channel email/SMS gagal anggun (INVALID_RECIPIENT).
//
// Isolasi tenant tetap struktural (pool sudah terkoneksi ke tenant DB); query tak menyebut
// tenant_id. Mutasi in-place pada slice (backing array sama dengan yang dikembalikan HoldersOf).
func (d *DBRecipientDirectory) fillContacts(ctx context.Context, recips []coreNotif.Recipient) error {
	if len(recips) == 0 {
		return nil
	}
	// Cek KEDUA kolom (email & no_hp): SELECT di bawah membaca keduanya, jadi bila hanya salah
	// satu ada (mis. ALTER terputus) fill dilewati alih-alih query gagal.
	// gov:raw-ok reason=notif-contact-probe query=notification-user-profiles-contact-cols
	var hasContactCols bool
	if err := d.pool.QueryRow(ctx,
		`SELECT count(*) = 2 FROM information_schema.columns
		    WHERE table_schema = 'gov' AND table_name = 'user_profiles'
		      AND column_name IN ('email', 'no_hp')`).
		Scan(&hasContactCols); err != nil {
		return fmt.Errorf("cek kolom kontak gov.user_profiles: %w", err)
	}
	if !hasContactCols {
		return nil
	}

	ids := make([]string, len(recips))
	for i, r := range recips {
		ids[i] = r.PersonID.String()
	}
	// gov:raw-ok reason=notif-contact-fill query=notification-contact-by-ids
	rows, err := d.pool.Query(ctx,
		`SELECT id, email, no_hp FROM gov.user_profiles WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("baca kontak user_profiles: %w", err)
	}
	defer rows.Close()

	type contact struct{ email, phone string }
	byID := make(map[uuid.UUID]contact, len(recips))
	for rows.Next() {
		var id uuid.UUID
		var email, phone *string // NULL → nil
		if err := rows.Scan(&id, &email, &phone); err != nil {
			return fmt.Errorf("scan kontak user_profiles: %w", err)
		}
		var c contact
		if email != nil {
			c.email = *email
		}
		if phone != nil {
			c.phone = *phone
		}
		byID[id] = c
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterasi kontak user_profiles: %w", err)
	}

	for i := range recips {
		if c, ok := byID[recips[i].PersonID]; ok {
			recips[i].Email = c.email
			recips[i].Phone = c.phone
		}
	}
	return nil
}

// withinScope menentukan apakah sebuah assignment berlaku untuk target unit kerja:
//   - assignment tenant-wide (unitKerjaID nil) → selalu berlaku, tanpa syarat.
//   - assignment di unit yang SAMA dengan target → selalu berlaku.
//   - assignment di unit LAIN → berlaku HANYA bila includeSubtree=true DAN target berada di
//     dalam subtree unit assignment (root=unit assignment, unit=target — arah yang sama dengan
//     permission.Hierarchy.IsWithin).
func (d *DBRecipientDirectory) withinScope(ctx context.Context, target uuid.UUID, assignmentUnit *uuid.UUID, includeSubtree bool) (bool, error) {
	if assignmentUnit == nil {
		return true, nil
	}
	if *assignmentUnit == target {
		return true, nil
	}
	if !includeSubtree {
		return false, nil
	}
	return d.hier.IsWithin(ctx, *assignmentUnit, target)
}

// ActingFor SENGAJA mengembalikan kosong (nil, nil) di PR-N1. Keputusan dikonfirmasi user
// (2026-07-26): PLT-jabatan (pelaksana tugas atas suatu jabatan/role) adalah konsep milik modul
// kepegawaian yang BELUM ADA. gov.delegations (delegation/) bersifat berbasis-PERMISSION
// ("user ini meminjam wewenang X,Y,Z"), BUKAN "user ini ditunjuk PLT sebagai Kadis" — menebak
// penunjukan PLT dari delegasi wewenang berisiko salah kirim notifikasi resmi ke orang yang
// sebenarnya cuma dipinjami sebagian izin, bukan menjabat.
//
// Konsekuensi: jabatan kosong (HoldersOf kosong) → Router.Resolve mengembalikan ErrNoRecipient
// (fail-loud), BUKAN salah kirim diam-diam ke penerima yang belum tentu tepat. Ini benar untuk
// keadaan sekarang. Mekanisme fallback PLT sendiri tetap teruji lewat MemoryDirectory.
//
// DEFERRED(Phase-7.x — modul kepegawaian): isi ActingFor dari sumber penunjukan PLT jabatan
// yang benar begitu modul kepegawaian tersedia. Lihat ROADMAP backlog "ActingFor PLT-jabatan".
func (d *DBRecipientDirectory) ActingFor(_ context.Context, _ coreNotif.RoleTarget) ([]coreNotif.Recipient, error) {
	return nil, nil
}
