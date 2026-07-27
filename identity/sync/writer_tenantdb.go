package sync

import (
	"context"
	stdsync "sync"

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
//
// ensured melacak tenant yang skemanya sudah dipastikan pada proses ini, sehingga DDL
// (termasuk ALTER yang mengambil ACCESS EXCLUSIVE lock) hanya jalan pada write PERTAMA per
// tenant — bukan tiap Upsert (yang akan men-serialize sync & memblok pembaca clone). Race
// jinak: dua goroutine bisa sama-sama ensure sekali (DDL idempoten).
type TenantDBWriter struct {
	pools   TenantPools
	ensured stdsync.Map // tenantID -> struct{}{}
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
	if _, done := w.ensured.Load(tenantID); !done {
		if err := ensureUserProfilesSchema(ctx, pool); err != nil {
			return err
		}
		w.ensured.Store(tenantID, struct{}{})
	}

	// NIP kosong (non-ASN) disimpan NULL. Kontak kosong juga NULL agar "kosong = kanal tak
	// tersedia" (Recipient) konsisten terbaca di sisi notifikasi.
	var nip, email, noHP any
	if c.NIP != "" {
		nip = c.NIP
	}
	if c.Email != "" {
		email = c.Email
	}
	if c.NoHP != "" {
		noHP = c.NoHP
	}
	const q = `INSERT INTO gov.user_profiles
	    (id, person_id, employment_status, nip, nik, nama_lengkap, assignment_id, is_cross_tenant, email, no_hp, synced_at)
	    VALUES ($1,$1,$2,$3,$4,$5,$6,$7,$8,$9, now())
	    ON CONFLICT (id) DO UPDATE SET
	        employment_status = EXCLUDED.employment_status,
	        nip               = EXCLUDED.nip,
	        nik               = EXCLUDED.nik,
	        nama_lengkap      = EXCLUDED.nama_lengkap,
	        assignment_id     = EXCLUDED.assignment_id,
	        is_cross_tenant   = EXCLUDED.is_cross_tenant,
	        email             = EXCLUDED.email,
	        no_hp             = EXCLUDED.no_hp,
	        synced_at         = now()`
	_, err = pool.Exec(ctx, q, c.PersonID, c.EmploymentStatus, nip, c.NIK,
		c.NamaLengkap, c.AssignmentID, c.IsCrossTenant, email, noHP)
	return err
}

// userProfilesDDL membuat schema gov + gov.user_profiles bila belum ada. Read-only clone
// dari identity (CLAUDE.md): id = person_id (anchor). Menyimpan KONTAK (email/no_hp) untuk
// routing notifikasi (PR-N3b, ADR-013) — masih TANPA kredensial/password (secret tetap di
// id.credentials). Kontak kelas personal_id; enkripsi field DEFERRED (ROADMAP 3.8).
//
// ALTER ... ADD COLUMN IF NOT EXISTS menambah kolom kontak pada tabel yang sudah dibuat
// versi lama (CREATE TABLE IF NOT EXISTS tak akan menambah kolom ke tabel yang sudah ada);
// keduanya idempoten sehingga ensure-on-write tetap aman dipanggil berulang.
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
    email             VARCHAR(255),
    no_hp             VARCHAR(15),
    synced_at         TIMESTAMPTZ NOT NULL,
    jabatan_lokal     VARCHAR(255),
    unit_kerja_id     UUID
);
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS email VARCHAR(255);
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS no_hp VARCHAR(15);`

func ensureUserProfilesSchema(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, userProfilesDDL)
	return err
}
