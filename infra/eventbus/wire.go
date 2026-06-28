package eventbus

import (
	"encoding/json"
	"fmt"

	"github.com/huda-salam/pamong/port"
)

// wireEvent adalah representasi serial saat event melewati transport eksternal
// (NATS, Redis Streams, dll). Name tidak disertakan karena sudah menjadi
// subject/key di sisi transport; driver mengisi Name dari konteks pengiriman.
type wireEvent struct {
	Payload        json.RawMessage `json:"payload"`
	TenantID       string          `json:"tenant_id,omitempty"`
	CausedBy       string          `json:"caused_by,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// marshalEvent menyerialisasi port.Event ke []byte untuk dikirim via transport.
// Payload di-marshal dua kali (inner JSON lalu outer wireEvent) sehingga tipe
// konkret payload tetap terepresentasi saat di-unmarshal kembali lewat schema.
func marshalEvent(event port.Event) ([]byte, error) {
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload event %q: %w", event.Name, err)
	}
	return json.Marshal(wireEvent{
		Payload:        payloadBytes,
		TenantID:       event.TenantID,
		CausedBy:       event.CausedBy,
		IdempotencyKey: event.IdempotencyKey,
	})
}

// unmarshalEvent merekonstruksi port.Event dari bytes transport. name adalah nama
// event yang diketahui dari subject/key transport; schema dipakai agar Payload
// kembali bertipe konkret (bukan map[string]any) sesuai yang didaftarkan modul.
func unmarshalEvent(data []byte, name string, schema *SchemaRegistry) (port.Event, error) {
	var wire wireEvent
	if err := json.Unmarshal(data, &wire); err != nil {
		return port.Event{}, fmt.Errorf("unmarshal wire event %q: %w", name, err)
	}
	payload, err := schema.Unmarshal(name, wire.Payload)
	if err != nil {
		return port.Event{}, err
	}
	return port.Event{
		Name:           name,
		Payload:        payload,
		TenantID:       wire.TenantID,
		CausedBy:       wire.CausedBy,
		IdempotencyKey: wire.IdempotencyKey,
	}, nil
}
