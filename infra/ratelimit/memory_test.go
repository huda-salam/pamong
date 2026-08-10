package ratelimit_test

import (
	"context"
	"strconv"
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

// Ruang key limiter dikendalikan pemanggil ANONIM (rute /auth/* sejak PR-W1), jadi tiga properti
// di bawah ini yang menjaganya tetap sehat di bawah banjir nilai unik.

// Jumlah jendela tak boleh tumbuh melewati batas meski key unik terus berdatangan dalam satu
// jendela waktu yang sama (tak ada yang kedaluwarsa untuk dibuang).
func TestMemory_JumlahEntriTerbatas(t *testing.T) {
	m := ratelimit.NewMemory(nil)
	ctx := context.Background()

	for i := 0; i < ratelimit.MaxEntriesForTest*3; i++ {
		if _, err := m.Allow(ctx, "flood:"+strconv.Itoa(i), 5, time.Hour); err != nil {
			t.Fatalf("Allow: %v", err)
		}
	}
	if n := m.LenForTest(); n > ratelimit.MaxEntriesForTest {
		t.Fatalf("jumlah jendela = %d, melebihi batas %d — banjir nilai unik dari pemanggil "+
			"anonim menumbuhkan memori limiter tanpa batas", n, ratelimit.MaxEntriesForTest)
	}
}

// Banjir key unik harus berbiaya O(1) per request. Dijaga lewat WAKTU, bukan lewat menghitung
// operasi: sebuah implementasi yang menyapu/mengevik per-insert (O(n) dengan n yang ikut tumbuh)
// membuat loop ini memakan puluhan detik. Ambangnya longgar agar tak rapuh di mesin lambat —
// yang dibedakan adalah linear vs kuadratik, bukan selisih beberapa persen.
func TestMemory_BanjirKeyUnik_TetapLinear(t *testing.T) {
	m := ratelimit.NewMemory(nil)
	ctx := context.Background()

	mulai := time.Now()
	for i := 0; i < ratelimit.MaxEntriesForTest*3; i++ {
		if _, err := m.Allow(ctx, "flood:"+strconv.Itoa(i), 5, time.Hour); err != nil {
			t.Fatalf("Allow: %v", err)
		}
	}
	if d := time.Since(mulai); d > 10*time.Second {
		t.Fatalf("300rb key unik makan %s — biaya per-request tumbuh bersama isi map "+
			"(sapuan/eviksi O(n) per insert), dan mutex-nya dibagi dengan lalu lintas rute bisnis", d)
	}
}

// Penghitung nyata bertahan melewati satu rotasi: entri yang masih dipakai dipromosikan dari
// generasi lama ke generasi berjalan, bukan dibuang.
func TestMemory_PenghitungBertahanLewatRotasi(t *testing.T) {
	now := time.Now()
	m := ratelimit.NewMemory(func() time.Time { return now })
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if ok, _ := m.Allow(ctx, "budi", 3, time.Hour); !ok {
			t.Fatalf("percobaan ke-%d harus lolos", i+1)
		}
	}
	// Paksa rotasi lewat waktu, lalu pastikan kuota "budi" TIDAK ikut ter-reset.
	now = now.Add(ratelimit.RotateIntervalForTest + time.Minute)
	if ok, _ := m.Allow(ctx, "budi", 3, time.Hour); ok {
		t.Fatal("kuota ter-reset oleh rotasi — penghitung di generasi lama tak dipromosikan, " +
			"jadi limiter bisa dilewati dengan menunggu rotasi")
	}
	if m.RotationsForTest() == 0 {
		t.Fatal("rotasi tak pernah terjadi; test tak menguji apa yang dimaksud")
	}
}
