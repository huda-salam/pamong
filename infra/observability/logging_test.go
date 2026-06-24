package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/port"
)

// TestLogger_JSONDenganCorrelationID memenuhi DoD PR-0.2.2:
// log keluar dalam format JSON dan menyertakan correlation ID dari context.
func TestLogger_JSONDenganCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NewLogger(observability.LogOptions{
		Level:  "info",
		Format: "json",
		Output: &buf,
	})

	ctx := port.WithCorrelationID(context.Background(), "req-abc-123")
	log.Info(ctx, "surat diterima", port.F("nomor_agenda", "2025/AG/00001"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output bukan JSON valid: %v\noutput: %s", err, buf.String())
	}
	if entry["msg"] != "surat diterima" {
		t.Errorf("msg = %v, mau 'surat diterima'", entry["msg"])
	}
	if entry["correlation_id"] != "req-abc-123" {
		t.Errorf("correlation_id = %v, mau 'req-abc-123'", entry["correlation_id"])
	}
	if entry["nomor_agenda"] != "2025/AG/00001" {
		t.Errorf("field nomor_agenda = %v, mau '2025/AG/00001'", entry["nomor_agenda"])
	}
}

// TestLogger_TanpaCorrelationID memastikan absennya correlation ID tidak menambah field kosong.
func TestLogger_TanpaCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NewLogger(observability.LogOptions{Format: "json", Output: &buf})
	log.Info(context.Background(), "tanpa cid")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output bukan JSON: %v", err)
	}
	if _, ada := entry["correlation_id"]; ada {
		t.Error("correlation_id tidak boleh ada bila context tidak menyertakannya")
	}
}

// TestLogger_LevelFilter memastikan level di bawah ambang tidak tercatat.
func TestLogger_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NewLogger(observability.LogOptions{Level: "warn", Format: "json", Output: &buf})

	log.Info(context.Background(), "ini info") // di bawah warn → tidak tercatat
	if buf.Len() != 0 {
		t.Errorf("info tidak boleh tercatat saat level=warn, dapat: %s", buf.String())
	}

	log.Warn(context.Background(), "ini warn")
	if buf.Len() == 0 {
		t.Error("warn harus tercatat saat level=warn")
	}
}

// TestLogger_With memastikan field permanen ikut di setiap log turunan.
func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NewLogger(observability.LogOptions{Format: "json", Output: &buf}).
		With(port.F("module", "surat_masuk"))
	log.Info(context.Background(), "halo")

	var entry map[string]any
	_ = json.Unmarshal(buf.Bytes(), &entry)
	if entry["module"] != "surat_masuk" {
		t.Errorf("field permanen module = %v, mau 'surat_masuk'", entry["module"])
	}
}
