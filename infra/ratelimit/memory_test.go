package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/huda-salam/pamong/infra/ratelimit"
)

func TestMemory_AllowsUpToLimitThenBlocks(t *testing.T) {
	now := time.Now()
	lim := ratelimit.NewMemory(func() time.Time { return now })

	for i := 0; i < 3; i++ {
		ok, err := lim.Allow(context.Background(), "k", 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("percobaan %d harus diizinkan: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, err := lim.Allow(context.Background(), "k", 3, time.Minute)
	if err != nil {
		t.Fatalf("err tak terduga: %v", err)
	}
	if ok {
		t.Fatal("percobaan ke-4 harus ditolak (melebihi limit)")
	}
}

func TestMemory_WindowResets(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	lim := ratelimit.NewMemory(clock)

	for i := 0; i < 3; i++ {
		_, _ = lim.Allow(context.Background(), "k", 3, time.Minute)
	}
	if ok, _ := lim.Allow(context.Background(), "k", 3, time.Minute); ok {
		t.Fatal("harus ditolak sebelum reset")
	}
	// Majukan waktu melewati jendela.
	now = now.Add(time.Minute + time.Second)
	if ok, _ := lim.Allow(context.Background(), "k", 3, time.Minute); !ok {
		t.Fatal("harus diizinkan setelah jendela reset")
	}
}

func TestMemory_KeysIsolated(t *testing.T) {
	now := time.Now()
	lim := ratelimit.NewMemory(func() time.Time { return now })

	if ok, _ := lim.Allow(context.Background(), "a", 1, time.Minute); !ok {
		t.Fatal("key a percobaan pertama harus diizinkan")
	}
	if ok, _ := lim.Allow(context.Background(), "a", 1, time.Minute); ok {
		t.Fatal("key a percobaan kedua harus ditolak")
	}
	if ok, _ := lim.Allow(context.Background(), "b", 1, time.Minute); !ok {
		t.Fatal("key b harus independen dari a")
	}
}

func TestMemory_ZeroLimitDenies(t *testing.T) {
	lim := ratelimit.NewMemory(nil)
	if ok, _ := lim.Allow(context.Background(), "k", 0, time.Minute); ok {
		t.Fatal("limit 0 harus selalu menolak")
	}
}
