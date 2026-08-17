package main

import (
	"testing"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/messaging"
	"github.com/huda-salam/pamong/port"
)

// testMessageSender membangun driver messaging `log` untuk e2e test. wireAuth kini menerima
// port.MessagingPort (bukan config), karena driver dirakit sekali di run() dan dibagi jalur OTP
// dan channel email notifikasi — lihat main.go.
func testMessageSender(t *testing.T) port.MessagingPort {
	t.Helper()
	sender, err := messaging.NewFromConfig(config.MessagingConfig{Driver: "log"})
	if err != nil {
		t.Fatalf("driver messaging test: %v", err)
	}
	return sender
}
