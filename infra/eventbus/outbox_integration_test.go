//go:build integration

package eventbus_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newIntegrationPool membuka pool Postgres menggunakan PAMONG_TEST_DB_DSN.
// Test di-skip bila env var tidak diset.
func newIntegrationPool(t *testing.T) (*db.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS gov.outbox_events; DROP SCHEMA IF EXISTS gov CASCADE`)
		pgpool.Close()
	})
	return pool, ctx
}

// newBusForIntegration membuat Bus memory dengan schema surat terdaftar.
func newBusForIntegration(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.NewMemory()
	if err := bus.Schema().Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return bus
}

// TestOutbox_KomitTransaksi_EventTerkirim membuktikan bahwa event yang ditulis ke
// outbox dalam transaksi yang berhasil commit akan di-dispatch oleh relay.
func TestOutbox_KomitTransaksi_EventTerkirim(t *testing.T) {
	pool, ctx := newIntegrationPool(t)
	bus := newBusForIntegration(t)

	if err := eventbus.EnsureOutboxSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var received []port.Event
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		received = append(received, e)
		return nil
	})

	// Tulis event ke outbox dalam transaksi yang di-commit.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := eventbus.NewOutboxStore(tx, bus.Schema())
	if err := store.Publish(ctx, port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "001/IN/2025"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	relay := eventbus.NewOutboxRelay(pool, bus, time.Second)
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("relay RunOnce: %v", err)
	}

	if len(received) != 1 {
		t.Fatalf("mau 1 event diterima, dapat %d", len(received))
	}
	sd, ok := received[0].Payload.(suratDiterima)
	if !ok {
		t.Fatalf("tipe payload salah: %T", received[0].Payload)
	}
	if sd.NomorSurat != "001/IN/2025" {
		t.Errorf("payload tidak utuh: %q", sd.NomorSurat)
	}
}

// TestOutbox_RollbackTransaksi_EventTidakTerkirim adalah DoD utama PR-3.1.2:
// membuktikan bahwa rollback transaksi mencegah event sampai ke subscriber.
func TestOutbox_RollbackTransaksi_EventTidakTerkirim(t *testing.T) {
	pool, ctx := newIntegrationPool(t)
	bus := newBusForIntegration(t)

	if err := eventbus.EnsureOutboxSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var received []port.Event
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		received = append(received, e)
		return nil
	})

	// Tulis event ke outbox lalu rollback — baris outbox harus ikut rollback.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := eventbus.NewOutboxStore(tx, bus.Schema())
	if err := store.Publish(ctx, port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "002/IN/2025"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish: %v", err)
	}
	// Rollback transaksi — event tidak boleh tersimpan.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	relay := eventbus.NewOutboxRelay(pool, bus, time.Second)
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("relay RunOnce: %v", err)
	}

	if len(received) != 0 {
		t.Errorf("rollback transaksi harus mencegah event terkirim, dapat %d event", len(received))
	}
}

// TestOutbox_EventSudahDispatched_TidakDikirimUlang memastikan idempotency relay:
// event yang sudah di-dispatch tidak dikirim ulang pada poll berikutnya.
func TestOutbox_EventSudahDispatched_TidakDikirimUlang(t *testing.T) {
	pool, ctx := newIntegrationPool(t)
	bus := newBusForIntegration(t)

	if err := eventbus.EnsureOutboxSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var count int
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error {
		count++
		return nil
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := eventbus.NewOutboxStore(tx, bus.Schema())
	if err := store.Publish(ctx, port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "003/IN/2025"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	relay := eventbus.NewOutboxRelay(pool, bus, time.Second)

	// Poll pertama: dispatch event.
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce pertama: %v", err)
	}
	// Poll kedua: tidak ada yang pending, count tetap 1.
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce kedua: %v", err)
	}

	if count != 1 {
		t.Errorf("event harus dikirim tepat sekali, dikirim %d kali", count)
	}
}

// TestOutbox_HandlerGagalTerusMenerus_MasukDLQ adalah DoD utama PR-3.1.4:
// membuktikan bahwa setelah MaxAttempts kali gagal, event masuk DLQ (failed_at di-set)
// dan tidak dipercobakan lagi oleh relay berikutnya.
func TestOutbox_HandlerGagalTerusMenerus_MasukDLQ(t *testing.T) {
	pool, ctx := newIntegrationPool(t)
	bus := newBusForIntegration(t)

	if err := eventbus.EnsureOutboxSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	dispatchErr := errors.New("layanan hilir tidak tersedia")
	callCount := 0
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error {
		callCount++
		return dispatchErr
	})

	// Tulis satu event ke outbox.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := eventbus.NewOutboxStore(tx, bus.Schema())
	if err := store.Publish(ctx, port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "DLQ/001/2025"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// RetryPolicy: MaxAttempts=3, BackoffBase=0 agar test tidak sleep.
	policy := eventbus.RetryPolicy{
		MaxAttempts: 3,
		BackoffBase: 0, // tanpa backoff sehingga tiap RunOnce langsung memproses
		BackoffMax:  time.Minute,
	}
	relay := eventbus.NewOutboxRelay(pool, bus, time.Hour).WithRetryPolicy(policy)

	// Jalankan relay sebanyak MaxAttempts kali — tiap cycle gagal.
	for i := 0; i < policy.MaxAttempts; i++ {
		if err := relay.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce siklus %d: %v", i+1, err)
		}
	}

	// Setelah MaxAttempts siklus, baris harus sudah di-DLQ (failed_at IS NOT NULL).
	var failedAt *time.Time
	row := pool.QueryRow(ctx,
		`SELECT failed_at FROM gov.outbox_events WHERE event_name = $1`, eventSuratDiterima)
	if err := row.Scan(&failedAt); err != nil {
		t.Fatalf("query failed_at: %v", err)
	}
	if failedAt == nil {
		t.Errorf("event harus masuk DLQ (failed_at di-set) setelah %d kali gagal, failed_at=NULL",
			policy.MaxAttempts)
	}

	// RunOnce tambahan tidak boleh memproses baris DLQ.
	prevCallCount := callCount
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce setelah DLQ: %v", err)
	}
	if callCount != prevCallCount {
		t.Errorf("relay tidak boleh memproses baris DLQ, callCount bertambah dari %d ke %d",
			prevCallCount, callCount)
	}
}

// TestOutbox_Backoff_EventTidakDiPollSebelumWaktuRetry membuktikan bahwa baris
// yang sedang dalam backoff (next_retry_at di masa depan) tidak di-poll oleh relay.
func TestOutbox_Backoff_EventTidakDiPollSebelumWaktuRetry(t *testing.T) {
	pool, ctx := newIntegrationPool(t)
	bus := newBusForIntegration(t)

	if err := eventbus.EnsureOutboxSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	dispatchCount := 0
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error {
		dispatchCount++
		return errors.New("gagal")
	})

	// Tulis event ke outbox.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := eventbus.NewOutboxStore(tx, bus.Schema())
	if err := store.Publish(ctx, port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "BACKOFF/001/2025"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// BackoffBase besar (1 jam) agar next_retry_at jauh di masa depan.
	policy := eventbus.RetryPolicy{
		MaxAttempts: 10,
		BackoffBase: time.Hour,
		BackoffMax:  24 * time.Hour,
	}
	relay := eventbus.NewOutboxRelay(pool, bus, time.Hour).WithRetryPolicy(policy)

	// Siklus pertama: Dispatch dipanggil (gagal), next_retry_at di-set ke ~1 jam mendatang.
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce pertama: %v", err)
	}
	if dispatchCount != 1 {
		t.Fatalf("siklus pertama harus dispatch sekali, dapat %d", dispatchCount)
	}

	// Siklus kedua: baris masih dalam backoff — tidak boleh di-poll.
	if err := relay.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce kedua: %v", err)
	}
	if dispatchCount != 1 {
		t.Errorf("siklus kedua tidak boleh dispatch saat masih backoff, dispatchCount=%d", dispatchCount)
	}
}
