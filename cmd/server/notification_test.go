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
