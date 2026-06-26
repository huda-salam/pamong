package sync_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
)

// fakeWriter merekam clone yang ditulis (atau memaksa error) tanpa DB.
type fakeWriter struct {
	calls []writeCall
	err   error
}

type writeCall struct {
	tenantID string
	clone    sync.UserProfileClone
}

func (w *fakeWriter) Upsert(_ context.Context, tenantID string, c sync.UserProfileClone) error {
	if w.err != nil {
		return w.err
	}
	w.calls = append(w.calls, writeCall{tenantID, c})
	return nil
}

// newBus menyiapkan memory bus dengan schema event ditugaskan terdaftar.
func newBus(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.NewMemory()
	if err := bus.Schema().Register(domain.EventEmploymentDitugaskan, domain.EmploymentDitugaskanPayload{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return bus
}

func ditugaskanEvent() port.Event {
	return port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: "pemkot-surabaya",
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID:     uuid.New(),
			EmploymentID:     uuid.New(),
			PersonID:         uuid.New(),
			TenantID:         "pemkot-surabaya",
			NIK:              "3578010101900001",
			NIP:              "199001012015011001",
			NamaLengkap:      "Budi",
			EmploymentStatus: "asn",
		},
	}
}

func TestEngine_ClonesOnDitugaskan(t *testing.T) {
	writer := &fakeWriter{}
	engine := sync.NewEngine(writer)
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}

	ev := ditugaskanEvent()
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(writer.calls) != 1 {
		t.Fatalf("harus satu clone ditulis, dapat %d", len(writer.calls))
	}
	got := writer.calls[0]
	want := ev.Payload.(domain.EmploymentDitugaskanPayload)
	if got.tenantID != "pemkot-surabaya" || got.clone.PersonID != want.PersonID ||
		got.clone.NIK != want.NIK || got.clone.NIP != want.NIP ||
		got.clone.EmploymentStatus != "asn" {
		t.Fatalf("clone tidak sesuai payload: %+v", got)
	}
}

func TestEngine_WriterErrorPropagates(t *testing.T) {
	writer := &fakeWriter{err: errors.New("tenant DB unreachable")}
	engine := sync.NewEngine(writer)
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Memory driver menggabungkan error handler dan mengembalikannya ke publisher.
	if err := bus.Publish(context.Background(), ditugaskanEvent()); err == nil {
		t.Fatal("kegagalan writer harus menggagalkan publish")
	}
}
