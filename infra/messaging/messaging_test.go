package messaging

import (
	"context"
	"errors"
	"net/smtp"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
)

func TestNewFromConfig_Log(t *testing.T) {
	m, err := NewFromConfig(config.MessagingConfig{Driver: "log"})
	if err != nil {
		t.Fatalf("driver log: err tak terduga: %v", err)
	}
	if _, ok := m.(*LogDriver); !ok {
		t.Fatalf("driver log: tipe = %T, mau *LogDriver", m)
	}
}

func TestNewFromConfig_DefaultKosongJadiLog(t *testing.T) {
	m, err := NewFromConfig(config.MessagingConfig{Driver: ""})
	if err != nil {
		t.Fatalf("driver kosong: err tak terduga: %v", err)
	}
	if _, ok := m.(*LogDriver); !ok {
		t.Fatalf("driver kosong: tipe = %T, mau *LogDriver (default)", m)
	}
}

func TestNewFromConfig_SMTP(t *testing.T) {
	m, err := NewFromConfig(config.MessagingConfig{
		Driver: "smtp", SMTPHost: "mail.example.test", FromEmail: "no-reply@example.test",
	})
	if err != nil {
		t.Fatalf("driver smtp: err tak terduga: %v", err)
	}
	if _, ok := m.(*SMTP); !ok {
		t.Fatalf("driver smtp: tipe = %T, mau *SMTP", m)
	}
}

func TestNewFromConfig_SMTPTanpaHost_Gagal(t *testing.T) {
	_, err := NewFromConfig(config.MessagingConfig{Driver: "smtp", FromEmail: "x@y.test"})
	if err == nil {
		t.Fatal("smtp tanpa host: mau error, dapat nil")
	}
}

func TestNewFromConfig_DriverTakDikenal_Gagal(t *testing.T) {
	_, err := NewFromConfig(config.MessagingConfig{Driver: "carrierpigeon"})
	if err == nil {
		t.Fatal("driver tak dikenal: mau error, dapat nil")
	}
}

func TestLogDriver_SelaluSukses(t *testing.T) {
	d := NewLogDriver(nil)
	if err := d.SendSMS(context.Background(), "0812", "kode 123"); err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if err := d.SendEmail(context.Background(), "a@b.test", "Subjek", "isi"); err != nil {
		t.Fatalf("SendEmail: %v", err)
	}
}

func TestSMTP_SendEmail_MemanggilSendMailDenganPesanTerbentuk(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	s := &SMTP{
		addr: "mail.example.test:587",
		from: "no-reply@example.test",
		sendMail: func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
			gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
			return nil
		},
	}
	if err := s.SendEmail(context.Background(), "warga@example.test", "Kôde OTP", "Kode Anda: 456"); err != nil {
		t.Fatalf("SendEmail: %v", err)
	}
	if gotAddr != "mail.example.test:587" {
		t.Errorf("addr = %q", gotAddr)
	}
	if gotFrom != "no-reply@example.test" {
		t.Errorf("from = %q", gotFrom)
	}
	if len(gotTo) != 1 || gotTo[0] != "warga@example.test" {
		t.Errorf("to = %v", gotTo)
	}
	msg := string(gotMsg)
	if !strings.Contains(msg, "To: warga@example.test\r\n") {
		t.Errorf("header To hilang:\n%s", msg)
	}
	if !strings.Contains(msg, "Kode Anda: 456") {
		t.Errorf("body hilang:\n%s", msg)
	}
	// Subject non-ASCII harus di-encode RFC 2047 (tidak muncul mentah).
	if strings.Contains(msg, "Kôde OTP") {
		t.Errorf("subject non-ASCII tak di-encode:\n%s", msg)
	}
}

func TestSMTP_SendEmail_AlamatKosong_InvalidRecipient(t *testing.T) {
	s := &SMTP{addr: "x:587", from: "f@t.test", sendMail: func(string, smtp.Auth, string, []string, []byte) error {
		t.Fatal("sendMail tak boleh dipanggil untuk alamat kosong")
		return nil
	}}
	err := s.SendEmail(context.Background(), "  ", "s", "b")
	var me *port.MessagingError
	if !errors.As(err, &me) || me.Code != port.MsgErrInvalidRecipient {
		t.Fatalf("mau MessagingError INVALID_RECIPIENT, dapat %v", err)
	}
}

func TestSMTP_SendEmail_HeaderInjection_Ditolak(t *testing.T) {
	s := &SMTP{addr: "x:587", from: "f@t.test", sendMail: func(string, smtp.Auth, string, []string, []byte) error {
		t.Fatal("sendMail tak boleh dipanggil untuk alamat ber-CRLF")
		return nil
	}}
	err := s.SendEmail(context.Background(), "victim@x.test\r\nBcc: mass@list.test", "s", "b")
	var me *port.MessagingError
	if !errors.As(err, &me) || me.Code != port.MsgErrInvalidRecipient {
		t.Fatalf("alamat ber-CRLF harus INVALID_RECIPIENT, dapat %v", err)
	}
}

func TestSMTP_SendEmail_GagalKirim_Transient(t *testing.T) {
	s := &SMTP{addr: "x:587", from: "f@t.test", sendMail: func(string, smtp.Auth, string, []string, []byte) error {
		return errors.New("connection refused")
	}}
	err := s.SendEmail(context.Background(), "a@b.test", "s", "b")
	var me *port.MessagingError
	if !errors.As(err, &me) || me.Code != port.MsgErrTransient {
		t.Fatalf("mau MessagingError TRANSIENT, dapat %v", err)
	}
}

func TestSMTP_SendSMS_TakDidukung_Permanent(t *testing.T) {
	s := &SMTP{addr: "x:587", from: "f@t.test"}
	err := s.SendSMS(context.Background(), "0812", "halo")
	var me *port.MessagingError
	if !errors.As(err, &me) || me.Code != port.MsgErrPermanent {
		t.Fatalf("mau MessagingError PERMANENT, dapat %v", err)
	}
}
