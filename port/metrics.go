package port

import "time"

// MetricsPort adalah seam observasi metrik untuk domain & adapter. Dua bentuk observasi
// "distribusi" disediakan TERPISAH per satuan (RecordDuration untuk waktu, RecordSize untuk
// byte) alih-alih satu Record generik: bucket histogram bergantung satuan, dan bucket detik
// (prometheus.DefBuckets: 0,005–10) membuat observasi byte menumpuk seluruhnya di +Inf —
// metrik yang ada tapi tak memberi informasi. Menyalurkan bucket lewat parameter ditolak
// karena membocorkan detail Prometheus ke port.
type MetricsPort interface {
	RecordDuration(name string, d time.Duration, tags map[string]string)

	// RecordSize mencatat ukuran (byte) ke histogram bersatuan byte. Dipakai untuk artefak
	// yang tumbuh seiring data — mis. ukuran token yang dibatasi pagar ADR-020 — sehingga
	// pertumbuhannya terlihat di dashboard SEBELUM menembus batas dan menjadi insiden.
	RecordSize(name string, bytes int, tags map[string]string)

	IncrCounter(name string, tags map[string]string)
	SetGauge(name string, v float64, tags map[string]string)
}
