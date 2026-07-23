// Package workflow menyediakan driven adapter Postgres untuk workflow.DefinitionStore.
// Seluruh kode yang menyentuh pgx HANYA ada di sini dan di infra/db — domain
// core/workflow tidak pernah mengimport infra (linter: domain-no-infra-import).
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

// workflowDefDDL membuat schema gov & tabel workflow_definitions bila belum ada.
// Identik dengan migration 001_create_workflow_definitions.up.sql — dipakai EnsureSchema
// untuk bootstrap langsung tanpa migration runner (mengikuti pola AuditRepo).
const workflowDefDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.workflow_definitions (
    workflow_id      TEXT        NOT NULL,
    version          INT         NOT NULL,
    entity           TEXT        NOT NULL DEFAULT '',
    initial_state    TEXT        NOT NULL,
    authoring_source TEXT        NOT NULL DEFAULT 'developer',
    states           JSONB       NOT NULL,
    transitions      JSONB       NOT NULL,
    effective_from   TIMESTAMPTZ NOT NULL,
    created_by       UUID,
    prev_version     INT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workflow_id, version)
);
CREATE INDEX IF NOT EXISTS idx_wfdef_lookup
    ON gov.workflow_definitions (workflow_id, version DESC);`

// DBStore mengimplementasi coreWf.DefinitionStore di atas Postgres.
// Setiap Register menciptakan versi baru — versi lama tetap tersimpan (append-only per
// workflow_id) sehingga instance yang sedang berjalan tetap mengacu ke versi saat mulai.
// Audit "siapa mengubah": gunakan RegisterAsActor untuk aksi admin; seed developer pakai
// Register (actorID = NULL di DB).
type DBStore struct {
	pool *db.Pool
}

// NewDBStore membuat store baru. Panggil EnsureSchema sebelum dipakai pertama kali.
func NewDBStore(pool *db.Pool) *DBStore { return &DBStore{pool: pool} }

var _ coreWf.DefinitionStore = (*DBStore)(nil)

// EnsureSchema membuat schema gov & tabel workflow_definitions bila belum ada.
// Dipanggil saat bootstrap — idempoten, aman dijalankan ulang.
func (s *DBStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, workflowDefDDL)
	return err
}

// Register memvalidasi dan menyimpan definisi sebagai versi baru.
// Bila workflow_id belum ada → versi 1. Bila sudah ada → versi (max+1).
// Actor NULL di DB — dipakai untuk seed developer. Untuk aksi admin: RegisterAsActor.
func (s *DBStore) Register(def coreWf.WorkflowDefinition) error {
	return s.registerVersion(context.Background(), def, nil)
}

// RegisterAsActor menyimpan versi baru sekaligus mencatat siapa pengubahnya.
// Dipakai use case admin saat meng-update definisi workflow secara eksplisit.
func (s *DBStore) RegisterAsActor(ctx context.Context, def coreWf.WorkflowDefinition, actorID uuid.UUID) error {
	return s.registerVersion(ctx, def, &actorID)
}

// registerVersion adalah implementasi inti: validasi → cek versi terakhir → INSERT baru.
func (s *DBStore) registerVersion(ctx context.Context, def coreWf.WorkflowDefinition, actorID *uuid.UUID) error {
	if err := coreWf.Validate(def); err != nil {
		return err
	}

	statesJSON, err := json.Marshal(def.States)
	if err != nil {
		return fmt.Errorf("serialisasi states workflow %q: %w", def.ID, err)
	}
	transitionsJSON, err := json.Marshal(def.Transitions)
	if err != nil {
		return fmt.Errorf("serialisasi transitions workflow %q: %w", def.ID, err)
	}

	// Cari versi tertinggi yang sudah ada; 0 jika belum ada sama sekali.
	var maxVer int
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM gov.workflow_definitions WHERE workflow_id = $1`,
		def.ID).Scan(&maxVer); err != nil {
		return fmt.Errorf("ambil versi maksimum workflow %q: %w", def.ID, err)
	}

	var prevVersion *int
	if maxVer > 0 {
		prevVersion = &maxVer
	}
	newVersion := maxVer + 1

	effectiveFrom := def.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = time.Now()
	}

	// gov:raw-ok reason=versioned-insert query=workflow-definition-new-version
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gov.workflow_definitions
		    (workflow_id, version, entity, initial_state, authoring_source,
		     states, transitions, effective_from, created_by, prev_version)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10)`,
		def.ID, newVersion, def.Entity, def.InitialState, def.AuthoringSource,
		statesJSON, transitionsJSON, effectiveFrom, actorID, prevVersion,
	)
	return err
}

// Get mengembalikan versi terbaru (version tertinggi) dari definisi workflow.
// ErrDefinitionNotFound bila workflow_id tidak ada sama sekali.
func (s *DBStore) Get(id string) (coreWf.WorkflowDefinition, error) {
	// gov:raw-ok reason=latest-version-query query=workflow-definition-get-latest
	return s.queryOne(context.Background(),
		`SELECT workflow_id, version, entity, initial_state, authoring_source,
		        states, transitions, effective_from
		 FROM gov.workflow_definitions
		 WHERE workflow_id = $1
		 ORDER BY version DESC LIMIT 1`,
		id)
}

// GetVersion mengembalikan versi spesifik. Dipakai engine saat instance mengunci versi
// tertentu (instance menyimpan DefinitionVersion saat dimulai — PRD F1/F7). Tanda tangan
// tanpa ctx agar memenuhi port DefinitionStore, konsisten dengan Get (context.Background).
func (s *DBStore) GetVersion(id string, version int) (coreWf.WorkflowDefinition, error) {
	// gov:raw-ok reason=pinned-version-query query=workflow-definition-get-version
	return s.queryOne(context.Background(),
		`SELECT workflow_id, version, entity, initial_state, authoring_source,
		        states, transitions, effective_from
		 FROM gov.workflow_definitions
		 WHERE workflow_id = $1 AND version = $2`,
		id, version)
}

func (s *DBStore) queryOne(ctx context.Context, q string, args ...any) (coreWf.WorkflowDefinition, error) {
	var (
		def             coreWf.WorkflowDefinition
		statesJSON      []byte
		transitionsJSON []byte
	)
	err := s.pool.QueryRow(ctx, q, args...).Scan(
		&def.ID, &def.Version, &def.Entity, &def.InitialState, &def.AuthoringSource,
		&statesJSON, &transitionsJSON, &def.EffectiveFrom,
	)
	if db.IsNoRows(err) {
		id := ""
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				id = s
			}
		}
		return coreWf.WorkflowDefinition{}, coreWf.ErrDefinitionNotFound(id)
	}
	if err != nil {
		return coreWf.WorkflowDefinition{}, fmt.Errorf("baca definisi workflow: %w", err)
	}
	if err := json.Unmarshal(statesJSON, &def.States); err != nil {
		return coreWf.WorkflowDefinition{}, fmt.Errorf("deserialisasi states: %w", err)
	}
	if err := json.Unmarshal(transitionsJSON, &def.Transitions); err != nil {
		return coreWf.WorkflowDefinition{}, fmt.Errorf("deserialisasi transitions: %w", err)
	}
	return def, nil
}
