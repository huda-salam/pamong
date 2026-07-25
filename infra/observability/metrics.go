// metrics.go implementasi port.MetricsPort di atas Prometheus client. Setiap
// nama metric membangun *Vec Prometheus secara lazy pada observasi pertama;
// nama label dikunci dari kunci tags saat itu (lihat labelSetFor). Konvensi
// tags (CLAUDE.md §metrics): module/tenant/endpoint — hindari label
// high-cardinality (id entitas dsb).
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
}

// labeledVec mengingat nama label yang dikunci saat *Vec dibuat, sehingga
// observasi berikutnya dengan set tag berbeda tetap bisa dipetakan (lihat values).
type labeledVec[V any] struct {
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
// pemanggil kelak mengirim kombinasi tag key yang tidak identik.
func values(labelNames []string, tags map[string]string) []string {
	vals := make([]string, len(labelNames))
	for i, name := range labelNames {
		vals[i] = tags[name]
	}
	return vals
}

func (p *PrometheusMetrics) IncrCounter(name string, tags map[string]string) {
	p.mu.Lock()
	lv, ok := p.counters[name]
	if !ok {
		labelNames := sortedLabelNames(tags)
		lv = &labeledVec[*prometheus.CounterVec]{
			labelNames: labelNames,
			vec: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: name,
				Help: name,
			}, labelNames),
		}
		p.reg.MustRegister(lv.vec)
		p.counters[name] = lv
	}
	p.mu.Unlock()

	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Inc()
}

func (p *PrometheusMetrics) SetGauge(name string, v float64, tags map[string]string) {
	p.mu.Lock()
	lv, ok := p.gauges[name]
	if !ok {
		labelNames := sortedLabelNames(tags)
		lv = &labeledVec[*prometheus.GaugeVec]{
			labelNames: labelNames,
			vec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: name,
				Help: name,
			}, labelNames),
		}
		p.reg.MustRegister(lv.vec)
		p.gauges[name] = lv
	}
	p.mu.Unlock()

	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Set(v)
}

func (p *PrometheusMetrics) RecordDuration(name string, d time.Duration, tags map[string]string) {
	p.mu.Lock()
	lv, ok := p.histograms[name]
	if !ok {
		labelNames := sortedLabelNames(tags)
		lv = &labeledVec[*prometheus.HistogramVec]{
			labelNames: labelNames,
			vec: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    name,
				Help:    name,
				Buckets: prometheus.DefBuckets, // detik; sesuai satuan Observe di bawah
			}, labelNames),
		}
		p.reg.MustRegister(lv.vec)
		p.histograms[name] = lv
	}
	p.mu.Unlock()

	lv.vec.WithLabelValues(values(lv.labelNames, tags)...).Observe(d.Seconds())
}
