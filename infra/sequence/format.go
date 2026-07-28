package sequence

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// tokenRE menangkap placeholder `{...}` di dalam pola nomor. Isi di luar kurung diteruskan
// apa adanya (teks literal seperti "/AG/").
var tokenRE = regexp.MustCompile(`\{([^{}]*)\}`)

// formatPattern merender pola nomor menjadi string final dengan menyulih placeholder:
//
//	{tahun}      → tahun (mis. 2025)
//	{nomor}      → nilai penghitung apa adanya (mis. 42)
//	{nomor:N}    → nilai penghitung di-zero-pad ke lebar N (mis. {nomor:5} → 00042)
//
// Teks lain diteruskan literal. Placeholder tak dikenal → error (fail loud): pola adalah
// data konfigurasi; typo lebih baik ketahuan di pintu masuk daripada menghasilkan nomor
// yang diam-diam salah. Fungsi ini murni (nol dependency infra) dan deterministik.
//
// Pola WAJIB memuat minimal satu `{nomor}`: penghitung DB tetap maju tiap pemanggilan, jadi
// pola tanpa `{nomor}` akan merender string konstan sementara counter bertambah → dua dokumen
// mendapat "nomor" identik (duplikat senyap). Ini ditolak, bukan dibiarkan.
//
// CATATAN identitas sequence (di luar fungsi ini): pemanggil di DBGenerator memakai `pattern`
// sebagai KUNCI penghitung (kolom gov.sequences.name). Konsekuensinya, pola bersifat IMMUTABLE
// per (tenant, tahun): mengubah format (mis. {nomor:5}→{nomor:6}) atau prefix meng-garpu counter
// ke baris baru yang mulai dari 1 → berpotensi mengulang nomor yang sudah terbit. Ubah pola hanya
// untuk periode/tahun baru.
func formatPattern(pattern string, current int64, tahun int) (string, error) {
	var (
		formatErr error
		sawNomor  bool
	)
	out := tokenRE.ReplaceAllStringFunc(pattern, func(match string) string {
		token := match[1 : len(match)-1] // buang kurung { }
		name, arg, hasArg := strings.Cut(token, ":")
		switch name {
		case "tahun":
			if hasArg {
				formatErr = fmt.Errorf("sequence: placeholder %q tidak menerima argumen", match)
				return match
			}
			return strconv.Itoa(tahun)
		case "nomor":
			sawNomor = true
			if !hasArg {
				return strconv.FormatInt(current, 10)
			}
			width, err := strconv.Atoi(arg)
			if err != nil || width < 0 {
				formatErr = fmt.Errorf("sequence: lebar tidak valid pada placeholder %q", match)
				return match
			}
			return fmt.Sprintf("%0*d", width, current)
		default:
			formatErr = fmt.Errorf("sequence: placeholder tidak dikenal %q pada pola %q", match, pattern)
			return match
		}
	})
	if formatErr != nil {
		return "", formatErr
	}
	if !sawNomor {
		return "", fmt.Errorf("sequence: pola %q tidak memuat {nomor} — akan menghasilkan nomor duplikat", pattern)
	}
	return out, nil
}
