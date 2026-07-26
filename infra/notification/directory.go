package notification

import (
	"context"
	"fmt"

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
// MemoryDirectory di produksi. In-app langsung jalan (cukup PersonID); Email/Phone kosong (seam
// kontak DEFERRED — lihat doc coreNotif.notification package).
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

	var out []coreNotif.Recipient
	for _, c := range candidates {
		if t.UnitKerjaID != nil {
			within, err := d.withinScope(ctx, *t.UnitKerjaID, c.unitKerjaID, c.includeSubtree)
			if err != nil {
				return nil, err
			}
			if !within {
				continue
			}
		}
		out = append(out, coreNotif.Recipient{PersonID: c.userID})
	}
	return out, nil
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
// DEFERRED(modul kepegawaian — PLT-jabatan): isi ActingFor dari sumber penunjukan PLT jabatan
// yang benar begitu modul kepegawaian tersedia. Lihat ROADMAP backlog "ActingFor PLT-jabatan".
func (d *DBRecipientDirectory) ActingFor(_ context.Context, _ coreNotif.RoleTarget) ([]coreNotif.Recipient, error) {
	return nil, nil
}
