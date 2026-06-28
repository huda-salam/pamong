package eventbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
)

// ----- mock port.DBConn -----

type mockConn struct {
	execErr   error
	execCalls []mockExecCall
}

type mockExecCall struct {
	sql  string
	args []any
}

func (m *mockConn) Exec(_ context.Context, sql string, args ...any) (port.CommandTag, error) {
	m.execCalls = append(m.execCalls, mockExecCall{sql: sql, args: args})
	return mockTag{}, m.execErr
}

func (m *mockConn) QueryRow(_ context.Context, _ string, _ ...any) port.Row {
	return nil
}

func (m *mockConn) Query(_ context.Context, _ string, _ ...any) (port.Rows, error) {
	return nil, nil
}

type mockTag struct{}

func (mockTag) RowsAffected() int64 { return 1 }

// ----- helper -----

func registryWithSurat(t *testing.T) *eventbus.SchemaRegistry {
	t.Helper()
	r := eventbus.NewSchemaRegistry()
	if err := r.Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return r
}

// ===== SchemaRegistry.Unmarshal =====

func TestUnmarshal_RekonstrUksiPayload(t *testing.T) {
	r := registryWithSurat(t)

	data, _ := json.Marshal(suratDiterima{NomorSurat: "001/IN/2025"})
	got, err := r.Unmarshal(eventSuratDiterima, data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sd, ok := got.(suratDiterima)
	if !ok {
		t.Fatalf("tipe hasil bukan suratDiterima, dapat: %T", got)
	}
	if sd.NomorSurat != "001/IN/2025" {
		t.Errorf("NomorSurat tidak utuh: %q", sd.NomorSurat)
	}
}

func TestUnmarshal_EventTakTerdaftar(t *testing.T) {
	r := eventbus.NewSchemaRegistry()
	_, err := r.Unmarshal(eventSuratDiterima, []byte(`{}`))
	if err == nil {
		t.Fatal("unmarshal event tak terdaftar harus gagal")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "VALIDATION_ERROR" {
		t.Errorf("mau VALIDATION_ERROR, dapat: %v", err)
	}
}

func TestUnmarshal_JSONInvalid(t *testing.T) {
	r := registryWithSurat(t)
	_, err := r.Unmarshal(eventSuratDiterima, []byte(`tidak-valid-json`))
	if err == nil {
		t.Fatal("unmarshal JSON tidak valid harus gagal")
	}
}

// ===== OutboxStore.Publish =====

func TestOutboxStore_Publish_TulisKeDB(t *testing.T) {
	conn := &mockConn{}
	store := eventbus.NewOutboxStore(conn, registryWithSurat(t))

	err := store.Publish(context.Background(), port.Event{
		Name:           eventSuratDiterima,
		Payload:        suratDiterima{NomorSurat: "001/IN/2025"},
		TenantID:       "pemkot-surabaya",
		CausedBy:       "user-123",
		IdempotencyKey: "idem-456",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(conn.execCalls) != 1 {
		t.Fatalf("mau 1 Exec call, dapat %d", len(conn.execCalls))
	}
	call := conn.execCalls[0]
	if !strings.Contains(call.sql, "gov.outbox_events") {
		t.Errorf("SQL harus menyebut gov.outbox_events, dapat: %s", call.sql)
	}
	// args: id, event_name, payload, tenant_id, caused_by, idempotency_key
	if len(call.args) != 6 {
		t.Errorf("mau 6 arg, dapat %d", len(call.args))
	}
	if call.args[1] != eventSuratDiterima {
		t.Errorf("arg event_name salah: %v", call.args[1])
	}
	if call.args[3] != "pemkot-surabaya" {
		t.Errorf("arg tenant_id salah: %v", call.args[3])
	}
}

func TestOutboxStore_Publish_EventTanpaSchemaDitolak(t *testing.T) {
	conn := &mockConn{}
	store := eventbus.NewOutboxStore(conn, eventbus.NewSchemaRegistry()) // registry kosong

	err := store.Publish(context.Background(), port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{},
	})
	if err == nil {
		t.Fatal("publish event tanpa schema harus ditolak sebelum menyentuh DB")
	}
	if len(conn.execCalls) != 0 {
		t.Error("Exec tidak boleh dipanggil jika schema tidak valid")
	}
}

func TestOutboxStore_Publish_DBErrorDiteruskan(t *testing.T) {
	sentinel := errors.New("koneksi terputus")
	conn := &mockConn{execErr: sentinel}
	store := eventbus.NewOutboxStore(conn, registryWithSurat(t))

	err := store.Publish(context.Background(), port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{},
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("error DB harus diteruskan, dapat: %v", err)
	}
}
