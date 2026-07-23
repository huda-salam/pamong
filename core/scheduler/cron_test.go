package scheduler_test

import (
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/scheduler"
)

func mustParse(t *testing.T, expr string) scheduler.Schedule {
	t.Helper()
	s, err := scheduler.ParseCron(expr)
	if err != nil {
		t.Fatalf("ParseCron(%q): %v", expr, err)
	}
	return s
}

func TestParseCron_InvalidRejectedAtParse(t *testing.T) {
	cases := []string{
		"",            // kosong
		"* * * *",     // kurang field
		"* * * * * *", // kelebihan field
		"60 * * * *",  // menit di luar rentang
		"* 24 * * *",  // jam di luar rentang
		"* * 0 * *",   // dom < 1
		"* * * 13 *",  // bulan > 12
		"*/0 * * * *", // langkah nol
		"5-2 * * * *", // rentang terbalik
		"abc * * * *", // bukan angka
	}
	for _, expr := range cases {
		if _, err := scheduler.ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q) seharusnya error", expr)
		}
	}
}

func TestParseCron_Macros(t *testing.T) {
	// @daily → tiap tengah malam.
	s := mustParse(t, "@daily")
	after := time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("@daily Next: want %v, got %v", want, next)
	}
}

func TestNext_EveryFiveMinutes(t *testing.T) {
	s := mustParse(t, "*/5 * * * *")
	after := time.Date(2026, 7, 23, 10, 2, 30, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next: want %v, got %v", want, next)
	}
}

func TestNext_SpecificTime(t *testing.T) {
	// 08:30 tiap hari.
	s := mustParse(t, "30 8 * * *")
	after := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next: want %v, got %v", want, next)
	}
}

func TestNext_StrictlyAfter(t *testing.T) {
	// Bila `after` tepat di waktu cocok, Next harus mengembalikan kejadian BERIKUTNYA.
	s := mustParse(t, "0 * * * *") // tiap jam bulat
	after := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next: want %v, got %v", want, next)
	}
}

func TestNext_DOM_OR_DOW(t *testing.T) {
	// Cron standar: bila dom & dow keduanya diset → OR. "0 0 1 * 1" = tgl 1 ATAU tiap Senin.
	s := mustParse(t, "0 0 1 * 1")
	// 2026-07-01 adalah Rabu (bukan Senin) tapi tanggal 1 → cocok.
	after := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next: want %v, got %v", want, next)
	}
}

func TestNext_ListAndRange(t *testing.T) {
	// Menit 0 & 30, jam 9-17.
	s := mustParse(t, "0,30 9-17 * * *")
	after := time.Date(2026, 7, 23, 17, 15, 0, 0, time.UTC)
	next := s.Next(after)
	want := time.Date(2026, 7, 23, 17, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next: want %v, got %v", want, next)
	}
	// Setelah 17:30 → besok 09:00.
	next2 := s.Next(want)
	want2 := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	if !next2.Equal(want2) {
		t.Errorf("Next2: want %v, got %v", want2, next2)
	}
}

func TestNext_DOW_Sunday_ZeroAndSeven(t *testing.T) {
	s0 := mustParse(t, "0 0 * * 0")
	s7 := mustParse(t, "0 0 * * 7")
	after := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC) // Kamis
	// 2026-07-26 adalah Minggu.
	want := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	if got := s0.Next(after); !got.Equal(want) {
		t.Errorf("dow=0 Next: want %v, got %v", want, got)
	}
	if got := s7.Next(after); !got.Equal(want) {
		t.Errorf("dow=7 Next: want %v, got %v", want, got)
	}
}
