// metrics.go implementasi port.MetricsPort di atas Prometheus client. Setiap
// nama metric membangun *Vec Prometheus secara lazy pada observasi pertama;
// nama label dikunci dari kunci tags saat itu (lihat sortedLabelNames/values).
// Konvensi tags (CLAUDE.md §metrics): module/tenant/endpoint — hindari label
// high-cardinality (id entitas dsb), dan jaga set kunci tags konsisten untuk
// satu nama metric (keterbatasan Prometheus: satu metric family = satu set
// label tetap; lihat komentar getOrRegister untuk perilaku saat dilanggar).
package observability

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/huda-salam/pamong/port"
)

// PrometheusMetrics mengimplementasi port.MetricsPort di atas registry
// Prometheus privat (bukan default global) agar aman dibuat berkali-kali,
// mis. satu instance per test, tanpa "duplicate metrics collector".
type PrometheusMetrics struct {
	reg *prometheus.Registry

	mu         sync.Mutex
	counters   map[string]*labeledVec[*prometheus.CounterVec]
	gauges     map[string]*labeledVec[*prometheus.GaugeVec]
	histograms map[string]*labeledVec[*prometheus.HistogramVec]
	// histUnit mengingat SATUAN yang mengklaim tiap nama histogram. Tanpa ini, RecordDuration &
	// RecordSize berbagi map histograms yang ber-key nama saja: nama yang dipakai keduanya akan
	// diam-diam memakai bucket yang terdaftar LEBIH DULU, sehingga observasi byte masuk bucket
	// detik dan menumpuk seluruhnya di +Inf. Penjaga tipe di getOrRegister tak menangkapnya —
	// keduanya *HistogramVec, jadi lookup map sudah pulang sebelum Register dipanggil. Itu persis
	// kegagalan "metriknya ada tapi tak menjawab apa pun" yang menjadi alasan port/metrics.go
	// memisahkan kedua method.
	histUnit map[string]string
}

// labeledVec mengingat nama label yang dikunci saat *Vec dibuat, sehingga
// observasi berikutnya dengan set tag berbeda tetap bisa dipetakan (lihat values).
type labeledVec[V prometheus.Collector] struct {
	labelNames []string
	vec        V
}

var _ port.MetricsPort = (*PrometheusMetrics)(nil)

// NewPrometheusMetrics membuat MetricsPort berbasis Prometheus dengan registry
// baru (bukan prometheus.DefaultRegisterer) agar terisolasi antar instance.
func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{
		reg:        prometheus.NewRegistry(),
		counters:   make(map[string]*labeledVec[*prometheus.CounterVec]),
		gauges:     make(map[string]*labeledVec[*prometheus.GaugeVec]),
		histograms: make(map[string]*labeledVec[*prometheus.HistogramVec]),
		histUnit:   make(map[string]string),
	}
}

// Handler mengembalikan http.Handler exposition Prometheus (format teks) untuk
// registry privat instance ini. Dipasang gateway ke GET /metrics saat router
// tersedia (DEFERRED(Phase-5.1.1) — lihat ROADMAP.md).
func (p *PrometheusMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{})
}

// sortedLabelNames mengembalikan kunci map tags terurut, agar nama label &
// urutan value pada *Vec deterministik lintas panggilan.
func sortedLabelNames(tags map[string]string) []string {
	names := make([]string, 0, len(tags))
	for k := range tags {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// values memetakan tags ke slice value sesuai labelNames yang sudah dikunci.
// Tag yang tidak ada di labelNames (set pertama) diabaikan; label yang tak
// disertakan di panggilan ini mendapat "" — dijamin tidak panic meski
// pemanggil kelak mengirim kombinasi tag key yang tidak identik dengan
// observasi pertama untuk nama metric yang sama.
func values(labelNames []string, tags map[string]string) []string {
	vals := make([]string, len(labelNames))
	for i, name := range labelNames {
		vals[i] = tags[name]
	}
	return vals
}

// getOrRegister mengembalikan *Vec yang sudah terdaftar untuk name, atau
// membuat & mendaftarkannya (via newVec) pada observasi pertama. Memakai
// Register (bukan MustRegister): bila name sebelumnya sudah dipakai untuk
// JENIS metric lain (mis. IncrCounter lalu RecordDuration dengan name yang
// sama — kesalahan pemanggil, Prometheus tak mengizinkan satu nama untuk dua
// jenis kolektor), Register gagal. Metrics adalah concern lintas-potong yang
// tidak boleh menjatuhkan transaksi bisnis pemanggil, jadi observasi tersebut
// (dan seterusnya untuk name yang sama) di-skip lewat entri nil, bukan panic
// (lihat TestPrometheusMetrics_NamaSamaJenisBerbeda_TidakPanic).
func getOrRegister[V prometheus.Collector](
	p *PrometheusMetrics,
	m map[string]*labeledVec[V],
	name string,
	tags map[string]string,
	newVec func(labelNames []string) V,
) *labeledVec[V] {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lv, ok := m[name]; ok {
		return lv // termasuk kasus lv == nil: percobaan sebelumnya gagal, jangan ulangi
	}

	labelNames := sortedLabelNames(tags)
	vec := newVec(labelNames)
	if err := p.reg.Register(vec); err != nil {
		if are, isAlready := err.(prometheus.AlreadyRegisteredError); isAlready {
			if existing, sameType := are.ExistingCollector.(V); sameType {
				lv := &labeledVec[V]{labelNames: labelNames, vec: existing}
				m[name] = lv
				return lv
			}
		}
		m[name] = nil
		return nil
	}

	lv := &labeledVec[V]{labelNames: labelNames, vec: vec}
	m[name] = lv
	return lv
}

// claimHistogramUnit menandai name sebagai milik satuan unit, atau menolak bila nama itu sudah
// diklaim satuan lain. Penolakan berarti observasi di-SKIP — sama seperti kegagalan registrasi:
// metrics adalah concern lintas-potong yang tak boleh menjatuhkan transaksi bisnis pemanggil.
// Dipanggil SEBELUM getOrRegister karena keduanya memakai p.mu yang tidak reentrant.
func (p *PrometheusMetrics) claimHistogramUnit(name, unit string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if have, ok := p.histUnit[name]; ok {
		return have == unit
	}
	p.histUnit[name] = unit
	return true
}

// Satuan histogram — lihat claimHistogramUnit.
const (
	unitSeconds = "seconds"
	unitBytes   = "bytes"
)

// IncrCounter menaikkan counter Prometheus bernama name sebesar satu.
func (p *PrometheusMetrics) IncrCounter(name string, tags map[string]string) {
	lv := getOrRegister(p, p.counters, name, tags, func(labelNames []string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: name}, labelNames)
	})
	if lv == nil {
		return
	}
	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Inc()
}

// SetGauge menetapkan nilai gauge Prometheus bernama name ke v (menimpa nilai
// sebelumnya, bukan mengakumulasi).
func (p *PrometheusMetrics) SetGauge(name string, v float64, tags map[string]string) {
	lv := getOrRegister(p, p.gauges, name, tags, func(labelNames []string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: name}, labelNames)
	})
	if lv == nil {
		return
	}
	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Set(v)
}

// RecordDuration mencatat durasi d (dalam detik) ke histogram Prometheus
// bernama name, memakai bucket default Prometheus (prometheus.DefBuckets).
func (p *PrometheusMetrics) RecordDuration(name string, d time.Duration, tags map[string]string) {
	if !p.claimHistogramUnit(name, unitSeconds) {
		return // nama ini sudah dipakai satuan lain (byte) — bucket-nya tak akan cocok
	}
	lv := getOrRegister(p, p.histograms, name, tags, func(labelNames []string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    name,
			Help:    name,
			Buckets: prometheus.DefBuckets, // detik; sesuai satuan Observe di bawah
		}, labelNames)
	})
	if lv == nil {
		return
	}
	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Observe(d.Seconds())
}

// RecordSize mencatat ukuran (byte) ke histogram Prometheus bernama name dengan bucket
// EKSPONENSIAL bersatuan byte (256 B … 64 KiB) — bukan prometheus.DefBuckets yang bersatuan
// detik. Rentang itu dipilih untuk artefak header/token: batas proxy yang lazim (nginx 8 KiB,
// ALB 16 KiB) jatuh di tengah bucket sehingga "seberapa dekat ke batas" terbaca dari grafik,
// bukan hanya "sudah lewat".
func (p *PrometheusMetrics) RecordSize(name string, bytes int, tags map[string]string) {
	if !p.claimHistogramUnit(name, unitBytes) {
		return // nama ini sudah dipakai satuan lain (detik) — bucket-nya tak akan cocok
	}
	lv := getOrRegister(p, p.histograms, name, tags, func(labelNames []string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    name,
			Help:    name,
			Buckets: prometheus.ExponentialBuckets(256, 2, 9), // 256 B, 512 B, … 64 KiB
		}, labelNames)
	})
	if lv == nil {
		return
	}
	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Observe(float64(bytes))
}
