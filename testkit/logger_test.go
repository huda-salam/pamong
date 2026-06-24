package testkit_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// TestNoopLogger memastikan NoopLogger memenuhi port.Logger tanpa panic.
func TestNoopLogger(t *testing.T) {
	var log port.Logger = testkit.NewNoopLogger()
	log.Info(context.Background(), "apa pun", port.F("k", "v"))
	log.With(port.F("module", "x")).Error(context.Background(), "err")
}

// TestCapturingLogger memastikan log terekam untuk diassert.
func TestCapturingLogger(t *testing.T) {
	log := testkit.NewCapturingLogger()
	log.Warn(context.Background(), "hampir SLA", port.F("instance", "wf-1"))

	if len(log.Entries) != 1 {
		t.Fatalf("entries = %d, mau 1", len(log.Entries))
	}
	e := log.Entries[0]
	if e.Level != "warn" || e.Message != "hampir SLA" {
		t.Errorf("entry = %+v, mau level=warn msg='hampir SLA'", e)
	}
}
