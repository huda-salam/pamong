package testkit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/port"
)

// --- MockRepo ---

// MockRepo adalah implementasi generik port.BaseRepository untuk unit test.
// Menyimpan entity di memori; tidak perlu DB.
type MockRepo[T any] struct {
	mu    sync.Mutex
	store map[uuid.UUID]*T
}

func NewMockRepo[T any]() *MockRepo[T] {
	return &MockRepo[T]{store: make(map[uuid.UUID]*T)}
}

func (r *MockRepo[T]) FindByID(_ context.Context, id uuid.UUID) (*T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[id]
	if !ok {
		return nil, core.ErrNotFound("entity", id.String())
	}
	cp := *v
	return &cp, nil
}

func (r *MockRepo[T]) Save(_ context.Context, entity *T) error {
	return nil // accept semua save; mock tidak menyimpan untuk menjaga test ringan
}

func (r *MockRepo[T]) Update(_ context.Context, entity *T) error { return nil }

// Seed menyimpan entity ke mock store agar FindByID bisa menemukannya.
func (r *MockRepo[T]) Seed(id uuid.UUID, entity *T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[id] = entity
}

// --- MockPublisher ---

// MockPublisher merekam event yang dipublikasikan untuk diassert di test.
type MockPublisher struct {
	mu     sync.Mutex
	events []port.Event
}

var _ port.EventPublisher = (*MockPublisher)(nil)

func NewMockPublisher() *MockPublisher { return &MockPublisher{} }

func (p *MockPublisher) Publish(_ context.Context, e port.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
	return nil
}

// Published mengembalikan salinan semua event yang telah dipublikasikan.
func (p *MockPublisher) Published() []port.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]port.Event, len(p.events))
	copy(cp, p.events)
	return cp
}

// AssertEventPublished memverifikasi bahwa event dengan nama tertentu telah dipublikasikan.
func AssertEventPublished(t *testing.T, pub *MockPublisher, eventName string) {
	t.Helper()
	for _, e := range pub.Published() {
		if e.Name == eventName {
			return
		}
	}
	t.Errorf("event %q tidak ditemukan dalam %v event yang dipublikasikan", eventName, len(pub.Published()))
}

// --- MockSequence ---

// MockSequence selalu mengembalikan nilai tetap yang ditentukan saat konstruksi.
type MockSequence struct{ val string }

var _ port.SequenceGenerator = (*MockSequence)(nil)

func NewMockSequence(val string) *MockSequence { return &MockSequence{val: val} }

func (s *MockSequence) Next(_ context.Context, _, _ string, _ int) (string, error) {
	return s.val, nil
}

// --- MockMetrics ---

// MockMetrics menerima semua panggilan metrik tanpa efek samping.
type MockMetrics struct{}

var _ port.MetricsPort = (*MockMetrics)(nil)

func NewMockMetrics() *MockMetrics { return &MockMetrics{} }

func (m *MockMetrics) RecordDuration(_ string, _ time.Duration, _ map[string]string) {}
func (m *MockMetrics) RecordSize(_ string, _ int, _ map[string]string)               {}
func (m *MockMetrics) IncrCounter(_ string, _ map[string]string)                     {}
func (m *MockMetrics) SetGauge(_ string, _ float64, _ map[string]string)             {}

// --- MockUserResolver ---

// MockUserResolver mengembalikan profil kosong; cukup untuk test yang tidak butuh data user.
type MockUserResolver struct{}

var _ port.UserResolver = (*MockUserResolver)(nil)

func NewMockUserResolver() *MockUserResolver { return &MockUserResolver{} }

func (r *MockUserResolver) ResolveByID(_ context.Context, id uuid.UUID) (*port.UserProfile, error) {
	return &port.UserProfile{ID: id, JabatanLokal: "Kepala Dinas"}, nil
}

func (r *MockUserResolver) ResolveByNIP(_ context.Context, _ string) (*port.UserProfile, error) {
	return &port.UserProfile{}, nil
}

func (r *MockUserResolver) ResolveByNIK(_ context.Context, _ string) (*port.UserProfile, error) {
	return &port.UserProfile{}, nil
}

func (r *MockUserResolver) IsCrossTenant(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (r *MockUserResolver) HasCentralRole(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, nil
}

// --- MockMessaging ---

// SentSMS & SentEmail merekam satu pesan terkirim untuk diassert di test.
type SentSMS struct{ To, Message string }
type SentEmail struct{ To, Subject, Body string }

// MockMessaging mengimplementasi port.MessagingPort di memori. Merekam semua pengiriman;
// FailEmail/FailSMS bila diset membuat pengiriman mengembalikan *port.MessagingError agar
// jalur kegagalan bisa diuji tanpa provider nyata.
type MockMessaging struct {
	mu        sync.Mutex
	SMS       []SentSMS
	Emails    []SentEmail
	FailEmail *port.MessagingError
	FailSMS   *port.MessagingError
}

func NewMockMessaging() *MockMessaging { return &MockMessaging{} }

var _ port.MessagingPort = (*MockMessaging)(nil)

func (m *MockMessaging) SendSMS(_ context.Context, phoneNumber, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailSMS != nil {
		return m.FailSMS
	}
	m.SMS = append(m.SMS, SentSMS{To: phoneNumber, Message: message})
	return nil
}

func (m *MockMessaging) SendEmail(_ context.Context, email, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailEmail != nil {
		return m.FailEmail
	}
	m.Emails = append(m.Emails, SentEmail{To: email, Subject: subject, Body: body})
	return nil
}

// SentEmails mengembalikan salinan email terkirim untuk assertion.
func (m *MockMessaging) SentEmails() []SentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SentEmail, len(m.Emails))
	copy(out, m.Emails)
	return out
}

// --- MockCrypto ---

// MockCrypto mengimplementasi port.CryptoPort di memori, tanpa KMS maupun DB (ADR-009).
// Dipakai test lapis di atas kripto (repository, audit) yang perlu tahu "field ini melewati
// enkripsi", bukan menguji kriptografinya — itu diuji di infra/crypto.
//
// BUKAN kripto: ciphertext-nya reversibel oleh siapa pun (base64). Sengaja demikian agar test
// bisa memeriksa bahwa nilai yang tersimpan bukan plaintext dan tetap bisa dibaca kembali.
// Sifat yang ditiru dari implementasi nyata:
//   - Encrypt memakai nonce acak → dua panggilan atas nilai sama berbeda hasilnya.
//   - Decrypt menolak ciphertext milik tenant lain ATAU baris lain (port.ErrCiphertextInvalid).
//   - Pengikatan tenant+baris disimpan sebagai DIGEST, bukan nilai terbaca — meniru sifat AAD
//     (ADR-016): blob tak bisa memberitahu ia milik baris mana, ia hanya bisa membenarkan atau
//     menolak koordinat yang disodorkan. Kalau recordID disimpan apa adanya lalu dibandingkan,
//     mock akan lolos untuk blob yang dipindah bersama "bukti" identitasnya sendiri — persis
//     alternatif yang ditolak ADR-016.
//   - BlindIndex deterministik per (tenant, purpose), TANPA recordID (ADR-016 §3), dan
//     menormalkan nilai dengan aturan YANG SAMA seperti implementasi nyata (trim + case-fold
//     untuk purpose tertentu). Kesamaan ini WAJIB: kalau mock lebih longgar, test keunikan
//     `_bidx` bisa lolos di sini lalu bentrok di produksi. Diikat test paritas di infra/crypto.
type MockCrypto struct{}

func NewMockCrypto() *MockCrypto { return &MockCrypto{} }

var _ port.CryptoPort = (*MockCrypto)(nil)

const mockCryptoPrefix = "mockenc"

func (c *MockCrypto) Encrypt(_ context.Context, ref port.FieldRef, plain []byte) ([]byte, error) {
	if ref.TenantID == "" || ref.Purpose == "" {
		return nil, errors.New("testkit: MockCrypto butuh tenantID & purpose")
	}
	if ref.RecordID == "" {
		return nil, errors.New("testkit: MockCrypto butuh RecordID (pengikatan baris, ADR-016)")
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return []byte(strings.Join([]string{
		mockCryptoPrefix, ref.Purpose, mockBindTag(ref.Row()),
		base64.RawStdEncoding.EncodeToString(nonce),
		base64.RawStdEncoding.EncodeToString(plain),
	}, ":")), nil
}

func (c *MockCrypto) Decrypt(_ context.Context, ref port.RowRef, ct []byte) ([]byte, error) {
	parts := strings.Split(string(ct), ":")
	if len(parts) != 5 || parts[0] != mockCryptoPrefix {
		return nil, fmt.Errorf("%w: bukan ciphertext MockCrypto", port.ErrCiphertextInvalid)
	}
	if ref.TenantID == "" || ref.RecordID == "" {
		return nil, errors.New("testkit: MockCrypto butuh tenantID & RecordID")
	}
	// Satu jawaban untuk tenant salah maupun baris salah — seperti implementasi nyata.
	if parts[2] != mockBindTag(ref) {
		return nil, fmt.Errorf("%w: ciphertext milik tenant/baris lain", port.ErrCiphertextInvalid)
	}
	plain, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, fmt.Errorf("%w: payload rusak", port.ErrCiphertextInvalid)
	}
	return plain, nil
}

// mockBindTag meniru peran AAD: ia mengikat blob ke (tenant, baris) tanpa membocorkan
// keduanya dalam bentuk terbaca. Ber-length-prefix karena alasan yang sama dengan AAD nyata —
// recordID tak selamanya UUID, dan pemisah polos membuat dua koordinat berbeda bisa
// menghasilkan tag yang sama.
func mockBindTag(ref port.RowRef) string {
	h := sha256.New()
	for _, part := range []string{ref.TenantID, ref.RecordID} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return base64.RawStdEncoding.EncodeToString(h.Sum(nil))
}

// PurposeOf membaca purpose dari ciphertext mock — meniru kontrak port.CryptoPort agar
// pemeriksaan "ciphertext dipindah antar kolom" di lapis repository bisa diuji tanpa KMS.
func (c *MockCrypto) PurposeOf(ct []byte) (string, error) {
	parts := strings.Split(string(ct), ":")
	if len(parts) != 5 || parts[0] != mockCryptoPrefix {
		return "", fmt.Errorf("%w: bukan ciphertext MockCrypto", port.ErrCiphertextInvalid)
	}
	return parts[1], nil
}

func (c *MockCrypto) BlindIndex(_ context.Context, tenantID, purpose string, plain []byte) ([]byte, error) {
	if tenantID == "" || purpose == "" {
		return nil, errors.New("testkit: MockCrypto butuh tenantID & purpose")
	}
	sum := sha256.Sum256([]byte(tenantID + "|" + purpose + "|" + normalizeBlindIndex(purpose, plain)))
	return sum[:], nil
}

// mockCaseFoldedPurposes menyalin tabel kebijakan framework di infra/crypto (crypto.go).
// Salinan, bukan import: testkit sengaja bebas dependency infra agar unit test domain tak
// menyeret pgx. Test paritas di infra/crypto yang menjaga keduanya tak menyimpang —
// menambah purpose di sana WAJIB diikuti di sini (test akan gagal bila lupa).
var mockCaseFoldedPurposes = map[string]bool{
	"email": true,
}

func normalizeBlindIndex(purpose string, plain []byte) string {
	out := strings.TrimSpace(string(plain))
	if mockCaseFoldedPurposes[purpose] {
		out = strings.ToLower(out)
	}
	return out
}

// --- MockDeadlineScheduler ---

// MockDeadlineScheduler merekam deadline SLA yang dijadwalkan/dibatalkan engine (PR-3.2.6).
// FailNext bila diset membuat panggilan BERIKUTNYA gagal (uji propagasi error wiring SLA).
type MockDeadlineScheduler struct {
	mu        sync.Mutex
	Scheduled []workflow.Deadline
	Cancelled []string
	FailNext  error
}

var _ workflow.DeadlineScheduler = (*MockDeadlineScheduler)(nil)

func NewMockDeadlineScheduler() *MockDeadlineScheduler { return &MockDeadlineScheduler{} }

func (m *MockDeadlineScheduler) ScheduleDeadline(_ context.Context, d workflow.Deadline) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		return err
	}
	m.Scheduled = append(m.Scheduled, d)
	return nil
}

func (m *MockDeadlineScheduler) CancelDeadline(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		return err
	}
	m.Cancelled = append(m.Cancelled, key)
	return nil
}

// ScheduledFor mengembalikan deadline terjadwal dengan key tertentu (dan true), atau false.
func (m *MockDeadlineScheduler) ScheduledFor(key string) (workflow.Deadline, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.Scheduled {
		if d.Key == key {
			return d, true
		}
	}
	return workflow.Deadline{}, false
}

// WasCancelled melaporkan apakah key tertentu telah dibatalkan.
func (m *MockDeadlineScheduler) WasCancelled(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.Cancelled {
		if k == key {
			return true
		}
	}
	return false
}

// --- MockInstanceStateReader ---

// MockInstanceStateReader mengembalikan state instance yang diset lewat Set — dipakai menguji
// guard race eskalasi (instance masih di state vs sudah pindah). ErrNotFound bila belum diset.
type MockInstanceStateReader struct {
	mu     sync.Mutex
	states map[uuid.UUID]string
}

var _ workflow.InstanceStateReader = (*MockInstanceStateReader)(nil)

func NewMockInstanceStateReader() *MockInstanceStateReader {
	return &MockInstanceStateReader{states: make(map[uuid.UUID]string)}
}

// Set menetapkan state terkini satu instance.
func (m *MockInstanceStateReader) Set(id uuid.UUID, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[id] = state
}

func (m *MockInstanceStateReader) CurrentState(_ context.Context, id uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[id]
	if !ok {
		return "", core.ErrNotFound("workflow instance", id.String())
	}
	return s, nil
}

// --- MockEscalator ---

// MockEscalator merekam eskalasi yang dipicu EscalationCoordinator (PR-3.2.6). FailNext bila
// diset membuat panggilan berikutnya gagal.
type MockEscalator struct {
	mu        sync.Mutex
	Escalated []workflow.Escalation
	FailNext  error
}

var _ workflow.Escalator = (*MockEscalator)(nil)

func NewMockEscalator() *MockEscalator { return &MockEscalator{} }

func (m *MockEscalator) Escalate(_ context.Context, e workflow.Escalation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		return err
	}
	m.Escalated = append(m.Escalated, e)
	return nil
}

// Count mengembalikan jumlah eskalasi yang terekam.
func (m *MockEscalator) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Escalated)
}

// --- MockTransitionNotifier ---

// notifyCall merekam satu panggilan NotifyTransition.
type notifyCall struct {
	TenantID string
	Spec     workflow.NotifySpec
	Instance workflow.WorkflowInstance
}

// MockTransitionNotifier merekam notifikasi transisi yang dipicu Engine (PR-N2). FailNext bila
// diset membuat panggilan berikutnya gagal (tapi tetap terekam — mencerminkan kegagalan
// transport, bukan kegagalan pra-dispatch).
type MockTransitionNotifier struct {
	mu       sync.Mutex
	Notified []notifyCall
	FailNext error
}

var _ workflow.TransitionNotifier = (*MockTransitionNotifier)(nil)

func NewMockTransitionNotifier() *MockTransitionNotifier { return &MockTransitionNotifier{} }

func (m *MockTransitionNotifier) NotifyTransition(_ context.Context, tenantID string, spec workflow.NotifySpec, inst workflow.WorkflowInstance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Notified = append(m.Notified, notifyCall{TenantID: tenantID, Spec: spec, Instance: inst})
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		return err
	}
	return nil
}

// Count mengembalikan jumlah notifikasi transisi yang terekam.
func (m *MockTransitionNotifier) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Notified)
}

// --- MockRoleChecker ---

// MockRoleChecker adalah RoleChecker in-memory (PR-N2 bagian C): role dianggap ada bila
// namanya ada di set Known (diabaikan per-tenant — cukup untuk unit test).
type MockRoleChecker struct {
	Known map[string]bool
}

var _ workflow.RoleChecker = (*MockRoleChecker)(nil)

// NewMockRoleChecker membuat checker dengan set role yang dikenal (variadic, boleh kosong).
func NewMockRoleChecker(known ...string) *MockRoleChecker {
	m := &MockRoleChecker{Known: make(map[string]bool, len(known))}
	for _, k := range known {
		m.Known[k] = true
	}
	return m
}

func (m *MockRoleChecker) RoleExists(_ context.Context, _, roleName string) (bool, error) {
	return m.Known[roleName], nil
}

// --- Assertion helpers ---

// IsPermissionDenied mengembalikan true jika err adalah ErrPermissionDenied framework.
func IsPermissionDenied(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "PERMISSION_DENIED"
}

// --- MemoryInstanceStore ---

// MemoryInstanceStore adalah workflow.InstanceStore in-memory untuk test (PR-W4a).
// Ia meniru dua sifat yang penting dari adapter DB — SALINAN nilai (bukan pointer bersama) dan
// optimistic locking terhadap Version — supaya test yang lulus di sini tidak menyembunyikan bug
// yang muncul di Postgres.
type MemoryInstanceStore struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]workflow.WorkflowInstance
	locked map[uuid.UUID]bool
}

var _ workflow.InstanceStore = (*MemoryInstanceStore)(nil)

func NewMemoryInstanceStore() *MemoryInstanceStore {
	return &MemoryInstanceStore{rows: make(map[uuid.UUID]workflow.WorkflowInstance)}
}

// Save menyimpan salinan instance bila Version-nya masih cocok dengan yang tersimpan.
func (m *MemoryInstanceStore) Save(_ context.Context, inst *workflow.WorkflowInstance) error {
	if inst == nil {
		return core.ErrValidation("workflow_instance", "instance nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, sudahAda := m.rows[inst.ID]
	if sudahAda && cur.Version != inst.Version {
		return core.ErrConflict("instance workflow sudah diubah pihak lain")
	}
	if !sudahAda {
		// Cermin uq_wfinst_entity_definition di adapter DB: satu alur per (definisi, entitas).
		// Mock yang membolehkannya akan meloloskan test untuk perilaku yang ditolak Postgres.
		for _, row := range m.rows {
			if row.DefinitionID == inst.DefinitionID && row.EntityID == inst.EntityID {
				return core.ErrConflict("entitas sudah punya instance untuk alur ini")
			}
		}
	}
	stored := *inst
	stored.Version = inst.Version + 1
	stored.History = append([]workflow.TransitionRecord(nil), inst.History...)
	stored.RoleBindings = cloneBindings(inst.RoleBindings)
	m.rows[inst.ID] = stored
	inst.Version = stored.Version
	return nil
}

// Get mengembalikan SALINAN instance tersimpan.
func (m *MemoryInstanceStore) Get(_ context.Context, id uuid.UUID) (*workflow.WorkflowInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, workflow.ErrInstanceNotFound(id.String())
	}
	out := row
	out.History = append([]workflow.TransitionRecord(nil), row.History...)
	out.RoleBindings = cloneBindings(row.RoleBindings)
	return &out, nil
}

// TryLockInstance menyerialisasi transisi per instance di dalam satu proses (padanan in-memory
// dari advisory lock Postgres). Tak menunggu: instance yang sedang dipegang menjawab ok=false.
func (m *MemoryInstanceStore) TryLockInstance(_ context.Context, id uuid.UUID) (func(), bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked == nil {
		m.locked = make(map[uuid.UUID]bool)
	}
	if m.locked[id] {
		return func() {}, false, nil
	}
	m.locked[id] = true
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.locked, id)
	}, true, nil
}

// cloneBindings menyalin map binding agar penyimpanan tak berbagi map dengan pemanggil — DB
// store nyata tak pernah berbagi memori, dan mock yang berbagi bisa menyembunyikan mutasi liar.
func cloneBindings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// CurrentState memenuhi workflow.InstanceStateReader (jalur guard race eskalasi).
func (m *MemoryInstanceStore) CurrentState(_ context.Context, id uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return "", workflow.ErrInstanceNotFound(id.String())
	}
	return row.CurrentState, nil
}
