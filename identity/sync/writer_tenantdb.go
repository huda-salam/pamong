package sync

import (
	"context"

	"github.com/huda-salam/pamong/infra/db"
)

// TenantPools adalah subset TenantConnManager yang dibutuhkan writer: pool ke DB satu
// tenant (lokasi dari registry, kredensial bersama). Diabstraksikan agar writer tak
// terikat tipe konkret dan mudah difake di test. *infra/db.TenantConnManager memenuhinya.
type TenantPools interface {
	Tenant(ctx context.Context, tenantID string) (*db.Pool, error)
}

// TenantDBWriter menulis gov.user_profiles ke DB tenant tujuan. Skema gov + tabel
// dipastikan ada lewat EnsureSchema-on-write (precedent identity.AuditStore untuk
// id.audit_logs): gov.user_profiles adalah tabel framework, belum termasuk migrasi modul.
type TenantDBWriter struct {
	pools TenantPools
}

var _ Writer = (*TenantDBWriter)(nil)

func NewTenantDBWriter(pools TenantPools) *TenantDBWriter {
	return &TenantDBWriter{pools: pools}
}

// Upsert idempoten: event clone bisa terkirim ulang (memory sinkron sekarang; NATS
// at-least-once kelak), jadi ON CONFLICT menyegarkan baris, bukan gagal.
func (w *TenantDBWriter) Upsert(ctx context.Context, tenantID string, c UserProfileClone) error {
	pool, err := w.pools.Tenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if err := ensureUserProfilesSchema(ctx, pool); err != nil {
		return err
	}

	// NIP kosong (non-ASN) disimpan NULL.
	var nip any
	if c.NIP != "" {
		nip = c.NIP
	}
	const q = `INSERT INTO gov.user_profiles
	    (id, person_id, employment_status, nip, nik, nama_lengkap, assignment_id, is_cross_tenant, synced_at)
	    VALUES ($1,$1,$2,$3,$4,$5,$6,$7, now())
	    ON CONFLICT (id) DO UPDATE SET
	        employment_status = EXCLUDED.employment_status,
	        nip               = EXCLUDED.nip,
	        nik               = EXCLUDED.nik,
	        nama_lengkap      = EXCLUDED.nama_lengkap,
	        assignment_id     = EXCLUDED.assignment_id,
	        is_cross_tenant   = EXCLUDED.is_cross_tenant,
	        synced_at         = now()`
	_, err = pool.Exec(ctx, q, c.PersonID, c.EmploymentStatus, nip, c.NIK,
		c.NamaLengkap, c.AssignmentID, c.IsCrossTenant)
	return err
}

// userProfilesDDL membuat schema gov + gov.user_profiles bila belum ada. Read-only clone
// dari identity (CLAUDE.md): TANPA kolom credential/password. id = person_id (anchor).
const userProfilesDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.user_profiles (
    id                UUID PRIMARY KEY,
    person_id         UUID NOT NULL,
    employment_status VARCHAR(10) NOT NULL,
    nip               VARCHAR(18),
    nik               VARCHAR(16) NOT NULL,
    nama_lengkap      VARCHAR(255) NOT NULL,
    assignment_id     UUID NOT NULL,
    is_cross_tenant   BOOLEAN NOT NULL DEFAULT false,
    synced_at         TIMESTAMPTZ NOT NULL,
    jabatan_lokal     VARCHAR(255),
    unit_kerja_id     UUID
);`

func ensureUserProfilesSchema(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, userProfilesDDL)
	return err
}
