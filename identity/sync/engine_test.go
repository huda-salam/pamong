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

// fakeSource memerankan CloneSource: mengembalikan pengenal tetap, merekam koordinat yang
// diminta, atau memaksa error (identity DB tak terbaca saat event ditangani).
type fakeSource struct {
	ids   sync.CloneAttributes
	err   error
	calls []sourceCall
}

type sourceCall struct{ personID, employmentID uuid.UUID }

func (s *fakeSource) Attributes(_ context.Context, personID, employmentID uuid.UUID) (sync.CloneAttributes, error) {
	s.calls = append(s.calls, sourceCall{personID, employmentID})
	if s.err != nil {
		return sync.CloneAttributes{}, s.err
	}
	return s.ids, nil
}

// Nama di source SENGAJA berbeda dari nama di payload: hanya dengan begitu test bisa
// membuktikan mana dari keduanya yang mendarat di clone.
func newSource() *fakeSource {
	return &fakeSource{ids: sync.CloneAttributes{
		NamaLengkap: "Budi Santoso, S.Kom.",
		NIK:         "3578010101900001", NIP: "199001012015011001",
		Email: "budi@example.test", NoHP: "0812340001",
	}}
}

// newEngine merakit Engine untuk test; konstruktor kini bisa gagal.
func newEngine(t *testing.T, w sync.Writer, s sync.CloneSource) *sync.Engine {
	t.Helper()
	engine, err := sync.NewEngine(w, s)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
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
			NamaLengkap:      "Budi",
			EmploymentStatus: "asn",
		},
	}
}

func TestEngine_ClonesOnDitugaskan(t *testing.T) {
	writer := &fakeWriter{}
	source := newSource()
	engine := newEngine(t, writer, source)
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
		got.clone.EmploymentStatus != "asn" {
		t.Fatalf("clone tidak sesuai payload: %+v", got)
	}
	// SELURUH isi clone berasal dari CloneSource, bukan dari event — termasuk NamaLengkap,
	// yang juga ada di payload. Satu baris clone tak boleh mencampur nilai saat event terbit
	// dengan nilai saat event ditangani; fixture sengaja membuat keduanya berbeda.
	if got.clone.NamaLengkap != source.ids.NamaLengkap {
		t.Fatalf("NamaLengkap clone harus dari CloneSource (%q), bukan dari payload: %q",
			source.ids.NamaLengkap, got.clone.NamaLengkap)
	}
	if got.clone.NIK != source.ids.NIK || got.clone.NIP != source.ids.NIP ||
		got.clone.Email != source.ids.Email || got.clone.NoHP != source.ids.NoHP {
		t.Fatalf("pengenal clone tidak berasal dari CloneSource: %+v", got.clone)
	}
	// Koordinat yang dipakai membaca harus dari event ITU, bukan nilai lain: source yang
	// dipanggil dengan person keliru mengembalikan pengenal orang lain tanpa satu pun error.
	if len(source.calls) != 1 || source.calls[0].personID != want.PersonID ||
		source.calls[0].employmentID != want.EmploymentID {
		t.Fatalf("CloneSource dipanggil dengan koordinat salah: %+v", source.calls)
	}
}

func TestEngine_WriterErrorPropagates(t *testing.T) {
	writer := &fakeWriter{err: errors.New("tenant DB unreachable")}
	engine := newEngine(t, writer, newSource())
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Memory driver menggabungkan error handler dan mengembalikannya ke publisher.
	if err := bus.Publish(context.Background(), ditugaskanEvent()); err == nil {
		t.Fatal("kegagalan writer harus menggagalkan publish")
	}
}

// TestEngine_SourceErrorTidakMenulisClone menegakkan kegagalan LANTANG: identity DB tak
// terbaca harus menahan event (retry), bukan menulis clone berpengenal kosong — baris yang
// lolos tanpa gejala tapi tak bisa di-resolve maupun dinotifikasi.
func TestEngine_SourceErrorTidakMenulisClone(t *testing.T) {
	writer := &fakeWriter{}
	source := newSource()
	source.err = errors.New("identity DB unreachable")
	engine := newEngine(t, writer, source)
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := bus.Publish(context.Background(), ditugaskanEvent()); err == nil {
		t.Fatal("kegagalan CloneSource harus menggagalkan publish")
	}
	if len(writer.calls) != 0 {
		t.Fatalf("tak boleh ada clone ditulis saat source gagal, dapat %d", len(writer.calls))
	}
}

// TestEngine_ButuhWriterDanSource: Engine setengah terpasang ditolak saat KONSTRUKSI, bukan
// saat event pertama tiba (cermin NewTenantDBWriter/NewFieldSealer).
func TestEngine_ButuhWriterDanSource(t *testing.T) {
	if _, err := sync.NewEngine(nil, newSource()); err == nil {
		t.Fatal("Writer nil harus ditolak")
	}
	if _, err := sync.NewEngine(&fakeWriter{}, nil); err == nil {
		t.Fatal("CloneSource nil harus ditolak")
	}
}
