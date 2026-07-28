package sequence

import "testing"

func TestFormatPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		current int64
		tahun   int
		want    string
		wantErr bool
	}{
		{"pola surat masuk (zero-pad)", "{tahun}/AG/{nomor:5}", 42, 2025, "2025/AG/00042", false},
		{"nomor tanpa lebar", "AG-{nomor}", 7, 2025, "AG-7", false},
		{"lebar lebih kecil dari nilai tak memotong", "{nomor:3}", 123456, 2025, "123456", false},
		{"tahun saja ditolak (tak konsumsi counter)", "{tahun}", 1, 2030, "", true},
		{"placeholder ganda + literal", "SK/{nomor:4}/{tahun}", 9, 2024, "SK/0009/2024", false},
		{"placeholder tak dikenal ditolak", "{bulan}/{nomor}", 1, 2025, "", true},
		{"tahun dengan argumen ditolak", "{tahun:2}", 1, 2025, "", true},
		{"lebar bukan angka ditolak", "{nomor:x}", 1, 2025, "", true},
		{"pola tanpa {nomor} ditolak (cegah duplikat)", "{tahun}/FIXED", 7, 2025, "", true},
		{"pola literal murni ditolak (tak konsumsi counter)", "STATIS", 5, 2025, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := formatPattern(c.pattern, c.current, c.tahun)
			if c.wantErr {
				if err == nil {
					t.Fatalf("formatPattern(%q) = %q, ingin error", c.pattern, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("formatPattern(%q) error tak terduga: %v", c.pattern, err)
			}
			if got != c.want {
				t.Fatalf("formatPattern(%q) = %q, ingin %q", c.pattern, got, c.want)
			}
		})
	}
}
