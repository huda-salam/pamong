package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/port"
)

// TestRunDue_TenantJobMasukKeContextHandler mengunci ADR-023 Keputusan 5: sejak tabel scheduler
// hidup di DB sentral, ctx yang tiba di handler adalah ctx loop proses-lebar — tanpa tenant.
// Satu-satunya sumber tenant adalah baris job, dan ia harus sampai lewat port.WithTenant supaya
// routing DB per-tenant di handler bekerja seperti pada jalur HTTP.
func TestRunDue_TenantJobMasukKeContextHandler(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)}
	var terlihat string
	reg := scheduler.NewRegistry()
	_ = reg.Register("tenant-peka", func(ctx context.Context, _ []byte) error {
		terlihat = port.TenantFrom(ctx)
		return nil
	})
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)

	ctx := context.Background()
	if _, err := r.Schedule(ctx, scheduler.ScheduledJob{
		TenantID: "pemkot-surabaya", Name: "sla", JobKey: "tenant-peka",
		Enabled: true, NextRunAt: clk.t,
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if n, err := r.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("RunDue = (%d, %v), mau (1, nil)", n, err)
	}
	if terlihat != "pemkot-surabaya" {
		t.Fatalf("tenant di ctx handler = %q, mau %q — handler akan menyentuh DB tenant yang keliru (atau gagal routing)",
			terlihat, "pemkot-surabaya")
	}
}

// TestInvoke_JobPlatformTakMewarisiTenantAmbient menutup arah kegagalan yang berlawanan, dan yang
// jauh lebih berbahaya: Trigger/Replay bisa dipanggil DARI DALAM request (tombol "jalankan
// sekarang" admin), sehingga ctx-nya sudah membawa tenant. Job level-platform (TenantID kosong)
// yang mewarisi tenant itu akan menulis ke DB tenant yang kebetulan sedang login — fail-open.
func TestInvoke_JobPlatformTakMewarisiTenantAmbient(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)}
	var terlihat = "belum-dipanggil"
	reg := scheduler.NewRegistry()
	_ = reg.Register("platform", func(ctx context.Context, _ []byte) error {
		terlihat = port.TenantFrom(ctx)
		return nil
	})
	r := newRunner(t, reg, scheduler.NewMemoryJobStore(), clk)

	// Pemanggil membawa tenant di ctx; job TIDAK.
	ctx := port.WithTenant(context.Background(), "pemkot-malang")
	if _, err := r.Trigger(ctx, "", "platform", nil); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if terlihat != "" {
		t.Fatalf("tenant di ctx handler = %q, mau kosong — job platform mewarisi tenant pemanggil", terlihat)
	}
}

// TestStart_DoneMenungguSiklusYangSedangBerjalan membuktikan shutdown yang BERSIH, bukan sekadar
// shutdown yang cepat: channel dari Start hanya boleh tertutup setelah handler yang sedang
// berjalan selesai. Tanpa sifat ini, run() menutup pool DB di bawah kaki job yang masih menulis
// riwayat — kelas cacat yang sama dengan Subscribe tanpa Flush (PR-3.1.3).
func TestStart_DoneMenungguSiklusYangSedangBerjalan(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)}

	masuk := make(chan struct{}) // handler memberi tahu bahwa ia sudah mulai
	lepas := make(chan struct{}) // test menahan handler sampai siap
	var mu sync.Mutex
	selesai := false

	reg := scheduler.NewRegistry()
	_ = reg.Register("lambat", func(context.Context, []byte) error {
		close(masuk)
		<-lepas
		mu.Lock()
		selesai = true
		mu.Unlock()
		return nil
	})
	store := scheduler.NewMemoryJobStore()
	// Interval sangat pendek agar tick pertama tiba tanpa membuat test lambat.
	r := scheduler.NewRunner(reg, store, 5*time.Millisecond).WithClock(clk.now)

	if _, err := r.Schedule(context.Background(), scheduler.ScheduledJob{
		TenantID: "t1", Name: "lambat", JobKey: "lambat", Enabled: true, NextRunAt: clk.t,
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := r.Start(ctx)

	select {
	case <-masuk:
	case <-time.After(2 * time.Second):
		t.Fatal("handler tak pernah dipanggil — loop scheduler tidak berjalan")
	}

	// Batalkan ctx SELAGI handler tertahan. done tidak boleh tertutup sekarang.
	cancel()
	select {
	case <-done:
		t.Fatal("done tertutup selagi handler masih berjalan — pemanggil akan menutup pool di bawah kaki job")
	case <-time.After(100 * time.Millisecond):
	}

	close(lepas)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done tak pernah tertutup setelah handler selesai — shutdown akan menggantung")
	}
	mu.Lock()
	defer mu.Unlock()
	if !selesai {
		t.Fatal("done tertutup tanpa handler menyelesaikan pekerjaannya")
	}
}
