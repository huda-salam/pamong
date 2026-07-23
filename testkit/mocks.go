package testkit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
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

// --- Assertion helpers ---

// IsPermissionDenied mengembalikan true jika err adalah ErrPermissionDenied framework.
func IsPermissionDenied(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "PERMISSION_DENIED"
}
