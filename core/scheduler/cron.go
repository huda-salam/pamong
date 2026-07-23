// Package scheduler adalah inti penjadwalan job: parsing ekspresi cron, registry
// handler ber-key, eksekusi & riwayat job. Zero dependency infra — persistensi
// (JobStore) dan lock terdistribusi diimplementasi di infra sebagai driven adapter.
//
// Prinsip yang sama dengan strategy/workflow: yang tersimpan di DB hanyalah PILIHAN
// (key job + ekspresi cron + payload), bukan logika. Logika job adalah kode Go
// ter-compile yang didaftarkan ke Registry. Ini menutup vektor "kode arbitrary di DB"
// (lihat CLAUDE.md — Fleksibilitas & titik ekstensi).
package scheduler

import (
	"strconv"
	"strings"
	"time"
)

// Schedule adalah ekspresi cron 5-field yang sudah ter-parse (menit jam dom bulan dow),
// siap dihitung waktu jalan berikutnya. Di-parse sekali saat schedule di-load sehingga
// syntax error ketahuan di pintu masuk, bukan saat runtime (pola guard DSL workflow).
type Schedule struct {
	expr    string
	minute  fieldSet // 0-59
	hour    fieldSet // 0-23
	dom     fieldSet // 1-31 (day of month)
	month   fieldSet // 1-12
	dow     fieldSet // 0-6  (0 = Minggu)
	domStar bool     // dom = "*" — kombinasi dom/dow memakai aturan OR ala cron standar
	dowStar bool     // dow = "*"
}

// fieldSet adalah himpunan nilai yang valid untuk satu field cron (bitmask 64-bit cukup
// untuk semua rentang: menit 0-59 muat di 64 bit).
type fieldSet uint64

func (fs fieldSet) has(v int) bool { return fs&(1<<uint(v)) != 0 }

// Expr mengembalikan ekspresi cron asli (untuk audit/tampilan).
func (s Schedule) Expr() string { return s.expr }

// macros memetakan alias umum ke ekspresi 5-field. Konservatif — hanya yang jelas
// maknanya untuk konteks pelaporan berkala pemerintahan.
var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// ParseCron mem-parse ekspresi cron 5-field (atau alias @daily/@hourly/dst) menjadi
// Schedule. Mendukung: '*', angka, rentang a-b, langkah */n & a-b/n, daftar a,b,c.
// Field dow menerima 0-6 (0=Minggu); 7 juga dipetakan ke Minggu untuk kompatibilitas.
func ParseCron(expr string) (Schedule, error) {
	raw := strings.TrimSpace(expr)
	if m, ok := macros[raw]; ok {
		raw = m
	}
	fields := strings.Fields(raw)
	if len(fields) != 5 {
		return Schedule{}, ErrInvalidCron(expr, "ekspresi harus 5 field (menit jam hari-bulan bulan hari-minggu) atau alias @daily/@hourly")
	}

	minute, err := parseField(fields[0], 0, 59, expr)
	if err != nil {
		return Schedule{}, err
	}
	hour, err := parseField(fields[1], 0, 23, expr)
	if err != nil {
		return Schedule{}, err
	}
	dom, err := parseField(fields[2], 1, 31, expr)
	if err != nil {
		return Schedule{}, err
	}
	month, err := parseField(fields[3], 1, 12, expr)
	if err != nil {
		return Schedule{}, err
	}
	dow, err := parseDOW(fields[4], expr)
	if err != nil {
		return Schedule{}, err
	}

	return Schedule{
		expr:    expr,
		minute:  minute,
		hour:    hour,
		dom:     dom,
		month:   month,
		dow:     dow,
		domStar: fields[2] == "*",
		dowStar: fields[4] == "*",
	}, nil
}

// parseField mem-parse satu field cron (daftar dipisah koma; tiap bagian: '*', angka,
// rentang, atau langkah) menjadi fieldSet dengan validasi rentang [min,max].
func parseField(field string, min, max int, expr string) (fieldSet, error) {
	var set fieldSet
	for _, part := range strings.Split(field, ",") {
		lo, hi, step, err := parsePart(part, min, max, expr)
		if err != nil {
			return 0, err
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << uint(v)
		}
	}
	if set == 0 {
		return 0, ErrInvalidCron(expr, "field kosong: "+field)
	}
	return set, nil
}

// parsePart menguraikan satu bagian: mengembalikan rentang efektif [lo,hi] + step.
func parsePart(part string, min, max int, expr string) (lo, hi, step int, err error) {
	step = 1
	rangePart := part
	if slash := strings.IndexByte(part, '/'); slash >= 0 {
		stepStr := part[slash+1:]
		rangePart = part[:slash]
		step, err = strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return 0, 0, 0, ErrInvalidCron(expr, "langkah tidak valid: "+part)
		}
	}

	switch {
	case rangePart == "*":
		lo, hi = min, max
	case strings.IndexByte(rangePart, '-') >= 0:
		dash := strings.IndexByte(rangePart, '-')
		lo, err = strconv.Atoi(rangePart[:dash])
		if err != nil {
			return 0, 0, 0, ErrInvalidCron(expr, "rentang tidak valid: "+part)
		}
		hi, err = strconv.Atoi(rangePart[dash+1:])
		if err != nil {
			return 0, 0, 0, ErrInvalidCron(expr, "rentang tidak valid: "+part)
		}
	default:
		lo, err = strconv.Atoi(rangePart)
		if err != nil {
			return 0, 0, 0, ErrInvalidCron(expr, "nilai tidak valid: "+part)
		}
		hi = lo
	}

	if lo < min || hi > max || lo > hi {
		return 0, 0, 0, ErrInvalidCron(expr, "nilai di luar rentang ["+strconv.Itoa(min)+","+strconv.Itoa(max)+"]: "+part)
	}
	return lo, hi, step, nil
}

// parseDOW mem-parse field day-of-week dengan normalisasi 7→0 (Minggu) sebelum masuk fieldSet.
func parseDOW(field, expr string) (fieldSet, error) {
	// Normalisasi angka 7 menjadi 0 (dua-duanya = Minggu) sebelum parse rentang biasa.
	set, err := parseField(field, 0, 7, expr)
	if err != nil {
		return 0, err
	}
	if set.has(7) {
		set &^= 1 << 7 // buang bit 7
		set |= 1 << 0  // set Minggu
	}
	return set, nil
}

// Next menghitung waktu jalan berikutnya yang cocok, strictly setelah `after`.
// Perhitungan dilakukan pada zona waktu `after` (pemanggil menetapkan lokasi tenant).
// Mengembalikan zero time bila tidak ada kecocokan dalam 4 tahun ke depan (mis. 31 Feb).
func (s Schedule) Next(after time.Time) time.Time {
	// Mulai dari menit berikutnya, buang detik/nanodetik (cron berpresisi menit).
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Batas aman: 4 tahun cakup semua kombinasi termasuk 29 Feb.
	limit := t.AddDate(4, 0, 0)
	for t.Before(limit) {
		if !s.month.has(int(t.Month())) {
			// Lompat ke awal bulan berikutnya.
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if !s.hour.has(t.Hour()) {
			t = t.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if !s.minute.has(t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

// dayMatches menerapkan aturan cron standar untuk kombinasi dom & dow:
//   - keduanya '*'   → cocok tiap hari
//   - salah satu '*' → dipakai yang non-'*'
//   - keduanya diset → cocok bila SALAH SATU cocok (OR), sesuai perilaku cron Vixie.
func (s Schedule) dayMatches(t time.Time) bool {
	domHit := s.dom.has(t.Day())
	dowHit := s.dow.has(int(t.Weekday()))
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowHit
	case s.dowStar:
		return domHit
	default:
		return domHit || dowHit
	}
}
