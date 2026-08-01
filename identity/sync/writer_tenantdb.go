package sync

import (
	"context"
	"fmt"
	stdsync "sync"

	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// Purpose kolom terenkripsi pada clone. Sama dengan purpose di identity DB (nama kolom
// logis) — tapi BUKAN kunci yang sama: realm-nya tenant, bukan RealmCentral. Lihat sealer().
const (
	purposeNIK   = "nik"
	purposeNIP   = "nip"
	purposeEmail = "email"
	purposeNoHP  = "no_hp"
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
//
// sealers meng-cache satu FieldSealer per tenant: realm kunci clone adalah TENANT-nya, bukan
// realm sentral tempat pengenal yang sama tersimpan di identity DB (lihat sealer()).
type TenantDBWriter struct {
	pools   TenantPools
	crypto  port.CryptoPort
	ensured stdsync.Map // tenantID -> struct{}{}
	sealers stdsync.Map // tenantID -> *crypto.FieldSealer
}

var _ Writer = (*TenantDBWriter)(nil)

// NewTenantDBWriter menolak CryptoPort nil. Clone membawa nik/nip/email/no_hp — seluruhnya
// kelas personal_id (ADR-009) — ke SETIAP tenant DB tempat person itu ditugaskan; writer tanpa
// kripto menyalinnya plaintext ke sana tanpa satu pun gejala. Gagal saat konstruksi, bukan saat
// baris pertama disalin.
func NewTenantDBWriter(pools TenantPools, c port.CryptoPort) (*TenantDBWriter, error) {
	if c == nil {
		return nil, fmt.Errorf("identity/sync: clone user_profiles butuh port.CryptoPort (pengenal terenkripsi, ADR-009)")
	}
	return &TenantDBWriter{pools: pools, crypto: c}, nil
}

// sealer mengembalikan sealer ber-realm TENANT untuk clone.
//
// Realmnya sengaja BUKAN crypto.RealmCentral meski nilainya person yang sama: clone hidup di
// DB tenant, jadi yang melindunginya harus kunci yang sama dengan sisa DB itu. Realm sentral
// di sini justru berarti satu kunci membuka clone SELURUH pemda sekaligus — dan dump satu
// tenant menjadi tuas untuk semua.
//
// Konsekuensi yang disengaja: bidx NIK yang sama berbeda antar tenant, sehingga clone tak bisa
// dipakai mengorelasikan orang lintas tenant. Tak ada yang hilang — lookup clone selalu di
// dalam satu tenant DB, dan UNIQUE(nik) global tetap ditegakkan di identity DB (ADR-017 §1).
func (w *TenantDBWriter) sealer(tenantID string) (*crypto.FieldSealer, error) {
	if s, ok := w.sealers.Load(tenantID); ok {
		return s.(*crypto.FieldSealer), nil
	}
	s, err := crypto.NewFieldSealer(w.crypto, tenantID, "identity/sync")
	if err != nil {
		return nil, err
	}
	actual, _ := w.sealers.LoadOrStore(tenantID, s)
	return actual.(*crypto.FieldSealer), nil
}

// Upsert idempoten: event clone bisa terkirim ulang (memory sinkron sekarang; NATS
// at-least-once kelak), jadi ON CONFLICT menyegarkan baris, bukan gagal.
//
// Catatan pengikatan baris (ADR-016): recordID = PersonID, yang juga kolom `id` clone. Upsert
// ON CONFLICT (id) menulis ulang baris dengan id yang sama, jadi ciphertext lama & baru terikat
// koordinat yang sama — tak ada baris yang berubah identitas di tengah jalan.
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

	seal, err := w.sealer(tenantID)
	if err != nil {
		return err
	}
	// Nilai kosong (NIP non-ASN, kontak tak terdaftar) menjadi NULL pada KEDUA kolom — Seal
	// yang menentukannya, bukan percabangan di sini. "Kosong = kanal tak tersedia" (Recipient)
	// tetap terbaca sama di sisi notifikasi.
	nikEnc, nikBidx, err := seal.Seal(ctx, purposeNIK, c.PersonID, c.NIK)
	if err != nil {
		return err
	}
	nipEnc, nipBidx, err := seal.Seal(ctx, purposeNIP, c.PersonID, c.NIP)
	if err != nil {
		return err
	}
	emailEnc, emailBidx, err := seal.Seal(ctx, purposeEmail, c.PersonID, c.Email)
	if err != nil {
		return err
	}
	noHPEnc, noHPBidx, err := seal.Seal(ctx, purposeNoHP, c.PersonID, c.NoHP)
	if err != nil {
		return err
	}

	const q = `INSERT INTO gov.user_profiles
	    (id, person_id, employment_status, nip_enc, nip_bidx, nik_enc, nik_bidx, nama_lengkap,
	     assignment_id, is_cross_tenant, email_enc, email_bidx, no_hp_enc, no_hp_bidx, synced_at)
	    VALUES ($1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now())
	    ON CONFLICT (id) DO UPDATE SET
	        employment_status = EXCLUDED.employment_status,
	        nip_enc           = EXCLUDED.nip_enc,
	        nip_bidx          = EXCLUDED.nip_bidx,
	        nik_enc           = EXCLUDED.nik_enc,
	        nik_bidx          = EXCLUDED.nik_bidx,
	        nama_lengkap      = EXCLUDED.nama_lengkap,
	        assignment_id     = EXCLUDED.assignment_id,
	        is_cross_tenant   = EXCLUDED.is_cross_tenant,
	        email_enc         = EXCLUDED.email_enc,
	        email_bidx        = EXCLUDED.email_bidx,
	        no_hp_enc         = EXCLUDED.no_hp_enc,
	        no_hp_bidx        = EXCLUDED.no_hp_bidx,
	        synced_at         = now()`
	_, err = pool.Exec(ctx, q, c.PersonID, c.EmploymentStatus, nipEnc, nipBidx, nikEnc, nikBidx,
		c.NamaLengkap, c.AssignmentID, c.IsCrossTenant, emailEnc, emailBidx, noHPEnc, noHPBidx)
	return err
}

// userProfilesDDL membuat schema gov + gov.user_profiles bila belum ada. Read-only clone
// dari identity (CLAUDE.md): id = person_id (anchor). Menyimpan KONTAK (email/no_hp) untuk
// routing notifikasi (PR-N3b, ADR-013) — masih TANPA kredensial/password (secret tetap di
// id.credentials).
//
// PR-3.8.5: nik/nip/email/no_hp TERENKRIPSI ({f}_enc) + blind index ({f}_bidx), realm kunci =
// TENANT. Kolom plaintext-nya DI-DROP, bukan dibiarkan berdampingan — meninggalkannya berarti
// dump tenant tetap terbaca dan seluruh pekerjaan ini hanya berhenti MENGISI kebocoran, bukan
// menutupnya (pendirian yang sama dengan migrasi 009 di identity DB).
//
// nama_lengkap SENGAJA tetap plaintext: kelas `personal`, bukan `personal_id` (ADR-009 §2) —
// ia harus bisa di-LIKE & di-ORDER BY, dan mengenkripsinya mematikan itu tanpa menutup apa pun
// yang belum tertutup (nama tidak mengidentifikasi seseorang secara unik).
//
// Bentuk ALTER-nya idempoten sehingga ensure-on-write aman dipanggil berulang:
//   - ADD COLUMN IF NOT EXISTS memasang kolom baru pada tabel yang dibuat versi lama (CREATE
//     TABLE IF NOT EXISTS tak menyentuh tabel yang sudah ada). Kolom baru dibiarkan NULLABLE
//     di jalur ALTER — NOT NULL untuk tabel berisi menuntut backfill, dan backfill bukan
//     pekerjaan ensure-on-write. Tenant BARU tetap mendapat NOT NULL dari CREATE TABLE.
//   - DROP COLUMN IF EXISTS membuang kolom plaintext. Pada tenant yang sudah punya baris, ini
//     MENGHILANGKAN nilai lama: clone adalah proyeksi, dan pemulihannya adalah menerbitkan
//     ulang event penugasan — bukan membaca balik kolom yang justru harus hilang.
//   - Index bidx sengaja NON-UNIQUE. Keunikan NIK/NIP ditegakkan di sumbernya (identity DB,
//     UNIQUE global); menegakkannya lagi di proyeksi hanya mengubah anomali sisi-sumber
//     menjadi sync yang macet di satu tenant.
const userProfilesDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.user_profiles (
    id                UUID PRIMARY KEY,
    person_id         UUID NOT NULL,
    employment_status VARCHAR(10) NOT NULL,
    nip_enc           BYTEA,
    nip_bidx          BYTEA,
    nik_enc           BYTEA NOT NULL,
    nik_bidx          BYTEA NOT NULL,
    nama_lengkap      VARCHAR(255) NOT NULL,
    assignment_id     UUID NOT NULL,
    is_cross_tenant   BOOLEAN NOT NULL DEFAULT false,
    email_enc         BYTEA,
    email_bidx        BYTEA,
    no_hp_enc         BYTEA,
    no_hp_bidx        BYTEA,
    synced_at         TIMESTAMPTZ NOT NULL,
    jabatan_lokal     VARCHAR(255),
    unit_kerja_id     UUID
);
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS nik_enc    BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS nik_bidx   BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS nip_enc    BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS nip_bidx   BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS email_enc  BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS email_bidx BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS no_hp_enc  BYTEA;
ALTER TABLE gov.user_profiles ADD COLUMN IF NOT EXISTS no_hp_bidx BYTEA;
ALTER TABLE gov.user_profiles DROP COLUMN IF EXISTS nik;
ALTER TABLE gov.user_profiles DROP COLUMN IF EXISTS nip;
ALTER TABLE gov.user_profiles DROP COLUMN IF EXISTS email;
ALTER TABLE gov.user_profiles DROP COLUMN IF EXISTS no_hp;
CREATE INDEX IF NOT EXISTS idx_user_profiles_nik_bidx ON gov.user_profiles (nik_bidx);
CREATE INDEX IF NOT EXISTS idx_user_profiles_nip_bidx ON gov.user_profiles (nip_bidx);`

func ensureUserProfilesSchema(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, userProfilesDDL)
	return err
}
