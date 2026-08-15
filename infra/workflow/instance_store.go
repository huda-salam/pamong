package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

// DBInstanceStore mengimplementasi coreWf.InstanceStore di atas Postgres (PR-W4a, ADR-022).
//
// Isolasi tenant STRUKTURAL: pool sudah terkoneksi ke satu tenant DB (konvensi yang sama dengan
// DBStore & DBTemplateStore di paket ini). Kolom tenant_id disimpan sebagai jejak, bukan sebagai
// filter keamanan — baris tenant lain secara fisik tak terjangkau lewat pool ini.
type DBInstanceStore struct {
	pool *db.Pool
}

// NewDBInstanceStore membuat store. Panggil EnsureSchema sebelum dipakai pertama kali.
func NewDBInstanceStore(pool *db.Pool) *DBInstanceStore { return &DBInstanceStore{pool: pool} }

var _ coreWf.InstanceStore = (*DBInstanceStore)(nil)

// EnsureSchema membuat schema gov & seluruh tabel workflow bila belum ada, dari SQL migrasi
// ter-embed (sumber tunggal) dengan pelacakan gov.migration_history di bawah advisory lock.
// Idempoten. Jalur produksi otoritatif = `pamongctl migrate`.
func (s *DBInstanceStore) EnsureSchema(ctx context.Context) error {
	return db.ApplyEmbeddedSchema(ctx, s.pool, coreWf.MigrationModule, coreWf.MigrationsFS)
}

// Save menyimpan instance di bawah OPTIMISTIC LOCKING terhadap inst.Version.
//
// Insert & update disatukan dalam satu pernyataan ber-guard, bukan "cek dulu lalu tulis": dua
// transisi bersamaan atas instance yang sama akan sama-sama lolos pemeriksaan terpisah, lalu
// keduanya menulis. Di sini yang kalah balapan tidak mengenai baris apa pun (klausa WHERE version
// gagal) dan menerima core.ErrConflict — bukan menimpa hasil transisi lawannya.
//
// Version di memori hanya dinaikkan SETELAH baris benar-benar tertulis, sehingga pemanggil yang
// menangani ErrConflict tidak memegang instance dengan versi yang tak pernah ada di DB.
func (s *DBInstanceStore) Save(ctx context.Context, inst *coreWf.WorkflowInstance) error {
	if inst == nil {
		return core.ErrValidation("workflow_instance", "instance nil")
	}
	bindingsJSON, err := json.Marshal(orEmptyMap(inst.RoleBindings))
	if err != nil {
		return fmt.Errorf("serialisasi role_bindings instance: %w", err)
	}
	historyJSON, err := json.Marshal(orEmptySlice(inst.History))
	if err != nil {
		return fmt.Errorf("serialisasi history instance: %w", err)
	}

	startedAt := inst.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	next := inst.Version + 1

	// gov:raw-ok reason=optimistic-locked-upsert query=workflow-instance-save
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO gov.workflow_instances
		    (id, tenant_id, definition_id, definition_version, entity_id, current_state,
		     role_bindings, history, version, started_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10, now())
		ON CONFLICT (id) DO UPDATE SET
		    current_state = EXCLUDED.current_state,
		    role_bindings = EXCLUDED.role_bindings,
		    history       = EXCLUDED.history,
		    version       = EXCLUDED.version,
		    updated_at    = now()
		WHERE gov.workflow_instances.version = $11`,
		inst.ID, inst.TenantID, inst.DefinitionID, inst.DefinitionVersion, inst.EntityID,
		inst.CurrentState, bindingsJSON, historyJSON, next, startedAt, inst.Version,
	)
	if db.IsUniqueViolation(err) {
		// uq_wfinst_entity_definition: entitas ini SUDAH punya instance untuk definisi tersebut.
		// Bukan kegagalan infra — ia jawaban domain ("alur ini sudah berjalan/selesai untuk
		// dokumen ini"), dan harus sampai ke klien sebagai 409, bukan 500.
		return core.ErrConflict(fmt.Sprintf(
			"entitas %s sudah punya instance untuk alur %s", inst.EntityID, inst.DefinitionID))
	}
	if err != nil {
		return fmt.Errorf("simpan instance workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return core.ErrConflict(fmt.Sprintf(
			"instance workflow %s sudah diubah pihak lain (versi %d bukan versi terkini)",
			inst.ID, inst.Version))
	}
	inst.Version = next
	return nil
}

// Get mengambil instance lengkap untuk melanjutkan transisi.
func (s *DBInstanceStore) Get(ctx context.Context, id uuid.UUID) (*coreWf.WorkflowInstance, error) {
	var (
		inst         coreWf.WorkflowInstance
		bindingsJSON []byte
		historyJSON  []byte
	)
	// gov:raw-ok reason=single-row-lookup query=workflow-instance-get
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, definition_id, definition_version, entity_id, current_state,
		       role_bindings, history, version, started_at
		FROM gov.workflow_instances
		WHERE id = $1`, id).Scan(
		&inst.ID, &inst.TenantID, &inst.DefinitionID, &inst.DefinitionVersion, &inst.EntityID,
		&inst.CurrentState, &bindingsJSON, &historyJSON, &inst.Version, &inst.StartedAt,
	)
	if db.IsNoRows(err) {
		return nil, coreWf.ErrInstanceNotFound(id.String())
	}
	if err != nil {
		return nil, fmt.Errorf("baca instance workflow: %w", err)
	}
	if err := json.Unmarshal(bindingsJSON, &inst.RoleBindings); err != nil {
		return nil, fmt.Errorf("deserialisasi role_bindings instance: %w", err)
	}
	if err := json.Unmarshal(historyJSON, &inst.History); err != nil {
		return nil, fmt.Errorf("deserialisasi history instance: %w", err)
	}
	return &inst, nil
}

// CurrentState membaca state terkini satu instance — jalur GUARD RACE eskalasi SLA
// (coreWf.InstanceStateReader). Sengaja query sempit, bukan Get lengkap: ia dipanggil setiap
// deadline jatuh tempo dan tak butuh history maupun binding.
func (s *DBInstanceStore) CurrentState(ctx context.Context, id uuid.UUID) (string, error) {
	var state string
	// gov:raw-ok reason=guard-race-narrow-read query=workflow-instance-current-state
	err := s.pool.QueryRow(ctx,
		`SELECT current_state FROM gov.workflow_instances WHERE id = $1`, id).Scan(&state)
	if db.IsNoRows(err) {
		return "", coreWf.ErrInstanceNotFound(id.String())
	}
	if err != nil {
		return "", fmt.Errorf("baca state instance workflow: %w", err)
	}
	return state, nil
}

// instanceLockTTL adalah umur SEWA kunci transisi.
//
// Dipilih jauh di atas durasi request yang wajar, dan itu disengaja: sewa yang kedaluwarsa saat
// action-nya MASIH berjalan akan membuka pintu bagi transisi kedua — persis kerusakan yang kunci
// ini cegah (satu surat terdisposisi dua kali). Batas atasnya adalah berapa lama satu instance
// boleh tersandera setelah proses pemegangnya mati; 5 menit adalah kompromi yang memberi ruang
// besar pada sisi keamanan sambil tetap pulih tanpa campur tangan operator.
const instanceLockTTL = 5 * time.Minute

// TryLockInstance mengambil kunci eksklusif per instance tanpa menunggu, sebagai BARIS ber-sewa
// (gov.workflow_instance_locks) — bukan sebagai lock tingkat sesi.
//
// Bentuk ini dipilih setelah versi advisory-lock terbukti salah secara liveness: `pg_try_advisory_
// xact_lock` menuntut transaksinya tetap terbuka selama kunci dipegang, sementara action yang
// berjalan di bawah kunci memakai POOL YANG SAMA. Setiap transisi karenanya menahan satu koneksi
// mati selama use case bekerja, dan tenant yang sibuk bisa menghabiskan pool-nya sendiri —
// menggantungkan seluruh request tenant itu, bukan hanya workflow. Di sini nol koneksi ditahan:
// acquire dan release masing-masing satu pernyataan singkat.
//
// Atomik lewat INSERT .. ON CONFLICT ber-guard kedaluwarsa (bentuk yang sama dengan
// infra/scheduler.DBLocker): tepat satu pemanggil yang menang saat balapan, yang kalah menerima
// nol baris → ok=false, bukan antre.
//
// Perbandingan & penetapan batas sewa memakai jam DATABASE (now()), bukan jam proses: dua replika
// dengan jam yang meleset tak boleh berbeda pendapat tentang kapan sebuah sewa habis.
//
// Cakupan kunci = satu tenant DB. Itu cakupan yang benar: instance tenant lain hidup di DB lain.
// Dua replika yang terhubung ke DB tenant yang sama berbagi tabel ini, jadi serialisasinya
// berlaku lintas proses — bukan hanya di dalam satu.
//
// Sewa membuat kunci ini TIDAK KEKAL: bila proses mati, kunci lepas sendiri setelah TTL (tak ada
// kunci yatim), tapi transisi yang berjalan lebih lama dari TTL kehilangan perlindungannya. Guard
// versi pada Save tetap menjadi jaring kedua di jalur itu — ia menolak penulis yang kalah,
// meski setelah action-nya terlanjur berjalan.
func (s *DBInstanceStore) TryLockInstance(ctx context.Context, id uuid.UUID) (func(), bool, error) {
	noop := func() {}
	var token uuid.UUID
	// gov:raw-ok reason=atomic-lease-acquire query=workflow-instance-lock-acquire
	err := s.pool.QueryRow(ctx, `
		INSERT INTO gov.workflow_instance_locks (instance_id, token, locked_until)
		VALUES ($1, $2, now() + make_interval(secs => $3))
		ON CONFLICT (instance_id) DO UPDATE
		    SET token = EXCLUDED.token, locked_until = EXCLUDED.locked_until
		    WHERE gov.workflow_instance_locks.locked_until < now()
		RETURNING token`,
		id, uuid.New(), instanceLockTTL.Seconds()).Scan(&token)
	if db.IsNoRows(err) {
		return noop, false, nil // masih dipegang & belum kedaluwarsa
	}
	if err != nil {
		return noop, false, fmt.Errorf("ambil kunci instance %s: %w", id, err)
	}

	// Release memakai context.WithoutCancel: kunci harus lepas meski request-nya sudah dibatalkan
	// (klien menutup koneksi di tengah transisi). Bila DELETE tetap gagal, kunci lepas sendiri
	// saat sewa habis — karena itu kegagalannya tak dipropagasi ke pemanggil.
	return func() {
		// gov:raw-ok reason=guarded-release query=workflow-instance-lock-release
		_, _ = s.pool.Exec(context.WithoutCancel(ctx),
			`DELETE FROM gov.workflow_instance_locks WHERE instance_id = $1 AND token = $2`,
			id, token)
	}, true, nil
}

// orEmptyMap & orEmptySlice menjaga kolom JSONB tetap berisi bentuk yang valid ('{}' / '[]')
// alih-alih literal null — pembaca lalu tak perlu membedakan "belum pernah diisi" dari "kosong".
func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func orEmptySlice(h []coreWf.TransitionRecord) []coreWf.TransitionRecord {
	if h == nil {
		return []coreWf.TransitionRecord{}
	}
	return h
}
