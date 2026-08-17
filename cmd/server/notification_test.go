package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	coreWf "github.com/huda-salam/pamong/core/workflow"
)

type gagalNotify struct{ dipanggil int }

func (g *gagalNotify) NotifyTransition(context.Context, string, coreWf.NotifySpec, coreWf.WorkflowInstance) error {
	g.dipanggil++
	return errors.New("template tak ditemukan")
}

type suksesNotify struct{ dipanggil int }

func (s *suksesNotify) NotifyTransition(context.Context, string, coreWf.NotifySpec, coreWf.WorkflowInstance) error {
	s.dipanggil++
	return nil
}

// TestTolerantTransitionNotifier_KegagalanTakMenjatuhkanTransisi mengunci keputusan yang paling
// mudah dibalik tanpa sengaja oleh orang berikutnya: kegagalan notifikasi TIDAK boleh menjadi
// error yang dilihat pemanggil.
//
// Pada titik notifier dipanggil, transisi sudah otoritatif dan sudah disimpan handler
// (gateway/workflow: Save dulu, baru execErr dikembalikan). Mengembalikan error dari sini berarti
// merespons 5xx atas transisi yang BERHASIL — dan respons wajar klien terhadap 5xx adalah retry,
// padahal aksi bisnisnya sudah dijalankan sekali. Template modul belum punya jalur seeding sama
// sekali, jadi ErrTemplateNotFound adalah keadaan normal hari ini, bukan insiden.
func TestTolerantTransitionNotifier_KegagalanTakMenjatuhkanTransisi(t *testing.T) {
	inner := &gagalNotify{}
	n := &tolerantTransitionNotifier{inner: inner} // metrics & logger nil: keduanya opsional

	err := n.NotifyTransition(context.Background(), "pemkot-x",
		coreWf.NotifySpec{ToRole: "agendaris", Template: "surat_selesai"},
		coreWf.WorkflowInstance{ID: uuid.New(), CurrentState: "selesai"})

	if err != nil {
		t.Fatalf("NotifyTransition mengembalikan %v — transisi yang sudah tersimpan akan direspons 5xx", err)
	}
	if inner.dipanggil != 1 {
		t.Fatalf("notifier dalam dipanggil %d kali, mau 1 — pembungkus tak boleh melewati pengiriman", inner.dipanggil)
	}
}

// TestTolerantTransitionNotifier_JalurSuksesDiteruskan memastikan pembungkusnya tidak berubah
// menjadi no-op: jalur sukses tetap benar-benar memanggil notifier di dalamnya.
func TestTolerantTransitionNotifier_JalurSuksesDiteruskan(t *testing.T) {
	inner := &suksesNotify{}
	n := &tolerantTransitionNotifier{inner: inner}

	if err := n.NotifyTransition(context.Background(), "pemkot-x",
		coreWf.NotifySpec{ToRole: "agendaris", Template: "surat_selesai"},
		coreWf.WorkflowInstance{ID: uuid.New()}); err != nil {
		t.Fatalf("jalur sukses: %v", err)
	}
	if inner.dipanggil != 1 {
		t.Fatalf("notifier dalam dipanggil %d kali, mau 1", inner.dipanggil)
	}
}

// TestTolerantTransitionNotifier_KegagalanPerakitanJugaDitoleransi menutup jalur yang lahir dari
// perakitan tertunda: sejak notifier dibangun saat transisi pertama (bukan saat runtime dirakit),
// kegagalan ensure-schema/seed notifikasi terjadi DI SINI — sesudah transisi tersimpan. Ia harus
// diperlakukan sama dengan kegagalan pengiriman, bukan diangkat sebagai error.
func TestTolerantTransitionNotifier_KegagalanPerakitanJugaDitoleransi(t *testing.T) {
	var dipanggil int
	n := &tolerantTransitionNotifier{
		build: func(context.Context) (coreWf.TransitionNotifier, error) {
			dipanggil++
			return nil, errors.New("schema notifikasi tak bisa dipastikan")
		},
	}
	if err := n.NotifyTransition(context.Background(), "pemkot-x",
		coreWf.NotifySpec{ToRole: "agendaris", Template: "surat_selesai"},
		coreWf.WorkflowInstance{ID: uuid.New()}); err != nil {
		t.Fatalf("kegagalan perakitan diangkat sebagai error: %v", err)
	}
	if dipanggil != 1 {
		t.Fatalf("build dipanggil %d kali, mau 1", dipanggil)
	}
}

// TestTolerantDeadlines_KegagalanTakMenjatuhkanTransisi mengunci sisi SLA dari keputusan yang
// sama. Sempat luput di putaran perbaikan pertama: notifikasi ditoleransi, penjadwalan deadline
// tidak — padahal keduanya dipanggil engine pada titik yang sama, sesudah transisi otoritatif.
func TestTolerantDeadlines_KegagalanTakMenjatuhkanTransisi(t *testing.T) {
	inner := &gagalDeadlines{}
	d := &tolerantDeadlines{inner: inner}

	if err := d.ScheduleDeadline(context.Background(), coreWf.Deadline{
		Key: "workflow.sla.x", Escalation: coreWf.Escalation{TenantID: "pemkot-x"},
	}); err != nil {
		t.Fatalf("ScheduleDeadline mengembalikan %v — transisi yang sudah tersimpan akan direspons 5xx", err)
	}
	if err := d.CancelDeadline(context.Background(), "workflow.sla.x"); err != nil {
		t.Fatalf("CancelDeadline mengembalikan %v", err)
	}
	if inner.schedule != 1 || inner.cancel != 1 {
		t.Fatalf("inner dipanggil schedule=%d cancel=%d, mau 1/1 — pembungkus tak boleh melewati penjadwalan",
			inner.schedule, inner.cancel)
	}
}

type gagalDeadlines struct{ schedule, cancel int }

func (g *gagalDeadlines) ScheduleDeadline(context.Context, coreWf.Deadline) error {
	g.schedule++
	return errors.New("DB sentral tak terjangkau")
}

func (g *gagalDeadlines) CancelDeadline(context.Context, string) error {
	g.cancel++
	return errors.New("DB sentral tak terjangkau")
}
