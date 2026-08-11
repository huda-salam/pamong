package db

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/port"
)

// AuditRepo mengimplementasi audit.Store di atas Postgres. Audit log append-only,
// dirantai hash per partisi (kolom tenant_id) untuk deteksi tamper (PR-1.3.2).
// Schema bisa berbeda: "gov" untuk audit tenant (chain per tenant), atau schema sentral
// seperti "id" untuk audit identity (chain tunggal lewat partisi konstan) — lihat ADR-003.
// conn bertipe TxConn, bukan *Pool: Append menuntut transaksi (advisory lock + baca chain +
// insert harus satu unit), sementara audit TENANT harus mengikuti DB-per-tenant. Dengan seam ini
// satu perakitan saat boot melayani semua tenant — pool dipilih dari tenant di context per
// panggilan (TenantRoutingConn) — tanpa Append kehilangan atomisitasnya. `*Pool` tetap memenuhi
// TxConn, jadi pemakaian satu-DB (pamongctl, audit sentral identity) tak berubah.
type AuditRepo struct {
	conn   TxConn
	schema string

	// ensured memo DB yang skemanya sudah dipastikan ada pada PROSES ini. Audit tenant hidup di
	// tiap DB tenant, dan tak ada satu titik boot yang bisa membuat tabelnya untuk semua tenant
	// (tenant ditemukan saat request, ADR-004) — jadi ia dipastikan saat penulisan PERTAMA ke DB
	// itu, lalu tidak lagi. Tanpa memo, setiap baris audit membayar satu DDL; tanpa ensure sama
	// sekali, mutasi pertama di tenant baru gagal di langkah audit.
	//
	// Kuncinya adalah tenant PERUTEAN (port.TenantFrom), bukan e.TenantID: yang menentukan DDL ini
	// mendarat di DB mana adalah tenant di context — itulah yang dibaca TenantRoutingConn. Kedua
	// nilai itu biasanya sama, tapi bila pernah berbeda (mis. entry lintas-tenant, partisi konstan
	// pada audit sentral) memo ber-kunci e.TenantID akan menandai DB yang salah sebagai "sudah
	// dipastikan" — dan penulisan berikutnya ke DB yang belum bertabel gagal, satu kali, dengan
	// pesan yang tak menyebut sebabnya. Kunci yang benar tak berbiaya, jadi tak ada alasan menawar.
	ensuredMu sync.Mutex
	ensured   map[string]bool
}

// NewAuditRepo membuat audit repo tenant di schema gov (chain per tenant_id).
func NewAuditRepo(conn TxConn) *AuditRepo { return &AuditRepo{conn: conn, schema: "gov"} }

// NewSchemaAuditRepo membuat audit repo pada schema tertentu (mis. "id" untuk identity
// sentral). Logika chain/verifikasi identik; hanya lokasi tabel yang berbeda.
func NewSchemaAuditRepo(conn TxConn, schema string) *AuditRepo {
	return &AuditRepo{conn: conn, schema: schema}
}

var (
	_ audit.Store      = (*AuditRepo)(nil)
	_ audit.QueryStore = (*AuditRepo)(nil) // jalur baca ber-gating (audit.Reader, PR-3.8.4)
)

var schemaIdentRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func (r *AuditRepo) table() string { return r.schema + ".audit_logs" }

// auditDDL menghasilkan DDL tabel audit untuk schema tertentu. schema berasal dari kode
// (konstanta), bukan input pengguna; tetap divalidasi sebagai identifier untuk aman.
func auditDDL(schema string) string {
	return fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %[1]s;
CREATE TABLE IF NOT EXISTS %[1]s.audit_logs (
    seq           BIGSERIAL,
    id            UUID PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    entity        TEXT NOT NULL,
    entity_id     UUID NOT NULL,
    action        TEXT NOT NULL,
    actor_id      UUID NOT NULL,
    actor_ip      TEXT NOT NULL DEFAULT '',
    diff          JSONB NOT NULL,
    workflow_from TEXT NOT NULL DEFAULT '',
    workflow_to   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL,
    prev_hash     TEXT NOT NULL,
    hash          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON %[1]s.audit_logs (entity, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON %[1]s.audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_seq ON %[1]s.audit_logs (tenant_id, seq);`, schema)
}

// EnsureSchema membuat schema & tabel audit bila belum ada. Dipanggil saat boot.
func (r *AuditRepo) EnsureSchema(ctx context.Context) error {
	if !schemaIdentRe.MatchString(r.schema) {
		return fmt.Errorf("schema audit tidak valid: %q", r.schema)
	}
	_, err := r.conn.Exec(ctx, auditDDL(r.schema))
	return err
}

// ensureFor memastikan tabel audit ada di DB yang dituju context ini, sekali per proses per DB.
// Untuk schema gov, DB dipilih dari tenant di context oleh TenantRoutingConn — jadi DDL mendarat
// di tempat yang benar tanpa parameter tenant, dan memonya dikunci dengan tenant yang sama
// (kosong = koneksi satu-DB: pamongctl, audit sentral identity).
func (r *AuditRepo) ensureFor(ctx context.Context) error {
	key := port.TenantFrom(ctx)
	r.ensuredMu.Lock()
	done := r.ensured[key]
	r.ensuredMu.Unlock()
	if done {
		return nil
	}
	if err := r.EnsureSchema(ctx); err != nil {
		return err
	}
	r.ensuredMu.Lock()
	if r.ensured == nil {
		r.ensured = make(map[string]bool)
	}
	r.ensured[key] = true
	r.ensuredMu.Unlock()
	return nil
}

// Append menyisipkan satu entry, merantainya ke entry terakhir dalam partisi yang sama
// (tenant_id). Penulisan diserialisasi per partisi lewat advisory lock transaksi agar
// chain tidak putus oleh penulisan paralel (PRD F3). Append-only: hanya INSERT.
func (r *AuditRepo) Append(ctx context.Context, e audit.AuditEntry) error {
	// DI LUAR transaksi: DDL di dalam tx yang memegang advisory lock akan memperpanjang lock
	// selama pembuatan tabel, dan kegagalannya menggagalkan penulisan yang sebenarnya sah.
	if err := r.ensureFor(ctx); err != nil {
		return err
	}
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op bila sudah commit

	// Serialisasi per partisi: pemegang lock berikutnya menunggu sampai commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, e.TenantID); err != nil {
		return err
	}

	// Hash entry terakhir partisi ini = prev_hash entry baru; seed bila belum ada.
	prev := audit.SeedHash
	var last string
	err = tx.QueryRow(ctx,
		`SELECT hash FROM `+r.table()+` WHERE tenant_id = $1 ORDER BY seq DESC LIMIT 1`,
		e.TenantID).Scan(&last)
	switch {
	case err == nil:
		prev = last
	case IsNoRows(err):
		// entry pertama: pakai seed
	default:
		return err
	}

	// Normalisasi timestamp ke presisi penyimpanan sebelum hashing agar konsisten
	// saat dibaca ulang untuk verifikasi.
	e.Timestamp = e.Timestamp.UTC().Truncate(time.Microsecond)
	e.PrevHash = prev
	e.Hash = audit.ComputeHash(prev, e)

	diffJSON, err := json.Marshal(e.Diff)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO `+r.table()+`
			(id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
			 diff, workflow_from, workflow_to, created_at, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.ID, e.TenantID, e.Entity, e.EntityID, string(e.Action), e.ActorID, e.ActorIP,
		diffJSON, e.WorkflowFrom, e.WorkflowTo, e.Timestamp, e.PrevHash, e.Hash,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ByEntity mengembalikan riwayat audit satu entity, terurut kronologis (F5).
func (r *AuditRepo) ByEntity(ctx context.Context, entity string, entityID uuid.UUID) ([]audit.AuditEntry, error) {
	return r.queryEntries(ctx,
		`WHERE entity = $1 AND entity_id = $2 ORDER BY seq ASC`, entity, entityID)
}

// ByTenant mengembalikan seluruh entry satu partisi (tenant_id) terurut chain (verifikasi).
func (r *AuditRepo) ByTenant(ctx context.Context, tenantID string) ([]audit.AuditEntry, error) {
	return r.queryEntries(ctx, `WHERE tenant_id = $1 ORDER BY seq ASC`, tenantID)
}

func (r *AuditRepo) queryEntries(ctx context.Context, where string, args ...any) ([]audit.AuditEntry, error) {
	rows, err := r.conn.Query(ctx, `
		SELECT id, tenant_id, entity, entity_id, action, actor_id, actor_ip,
		       diff, workflow_from, workflow_to, created_at, prev_hash, hash
		FROM `+r.table()+` `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []audit.AuditEntry
	for rows.Next() {
		var e audit.AuditEntry
		var action string
		var diffJSON []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Entity, &e.EntityID, &action,
			&e.ActorID, &e.ActorIP, &diffJSON, &e.WorkflowFrom, &e.WorkflowTo,
			&e.Timestamp, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.Action = audit.Action(action)
		if err := json.Unmarshal(diffJSON, &e.Diff); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
