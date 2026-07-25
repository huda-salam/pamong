package observability_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huda-salam/pamong/infra/observability"
)

// scrape memanggil Handler() lewat httptest dan mengembalikan body exposition
// Prometheus (format teks) untuk diperiksa test.
func scrape(t *testing.T, m *observability.PrometheusMetrics) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("scrape status = %d, mau 200", rec.Code)
	}
	return rec.Body.String()
}

// TestPrometheusMetrics_CounterTereskpos memenuhi DoD PRD PR-3.7.2: metric
// tereskpos di endpoint (di sini: body exposition Handler()).
func TestPrometheusMetrics_CounterTereskpos(t *testing.T) {
	m := observability.NewPrometheusMetrics()
	m.IncrCounter("surat_masuk_dibuat_total", map[string]string{"module": "surat_masuk", "tenant": "t1"})
	m.IncrCounter("surat_masuk_dibuat_total", map[string]string{"module": "surat_masuk", "tenant": "t1"})

	body := scrape(t, m)
	if !strings.Contains(body, "surat_masuk_dibuat_total") {
		t.Fatalf("body tidak memuat nama metric, dapat:\n%s", body)
	}
	if !strings.Contains(body, `tenant="t1"`) {
		t.Errorf("body tidak memuat label tenant, dapat:\n%s", body)
	}
	if !strings.Contains(body, "surat_masuk_dibuat_total{module=\"surat_masuk\",tenant=\"t1\"} 2") {
		t.Errorf("counter harus bernilai 2 setelah dua IncrCounter, dapat:\n%s", body)
	}
}

func TestPrometheusMetrics_Gauge(t *testing.T) {
	m := observability.NewPrometheusMetrics()
	m.SetGauge("antrean_pending", 5, map[string]string{"module": "keuangan"})
	m.SetGauge("antrean_pending", 3, map[string]string{"module": "keuangan"}) // overwrite, bukan akumulasi

	body := scrape(t, m)
	if !strings.Contains(body, `antrean_pending{module="keuangan"} 3`) {
		t.Errorf("gauge harus bernilai 3 (nilai terakhir), dapat:\n%s", body)
	}
}

func TestPrometheusMetrics_RecordDuration_Histogram(t *testing.T) {
	m := observability.NewPrometheusMetrics()
	m.RecordDuration("http_request_duration_seconds", 150*time.Millisecond, map[string]string{"endpoint": "/surat"})

	body := scrape(t, m)
	if !strings.Contains(body, "http_request_duration_seconds_bucket") {
		t.Fatalf("histogram harus expose bucket, dapat:\n%s", body)
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{endpoint="/surat"} 1`) {
		t.Errorf("count histogram harus 1 setelah satu observasi, dapat:\n%s", body)
	}
}

// TestPrometheusMetrics_TagKeyBerbeda_TidakPanic membuktikan set tag key yang
// berbeda dari observasi pertama tidak panic — label yang hilang default "".
func TestPrometheusMetrics_TagKeyBerbeda_TidakPanic(t *testing.T) {
	m := observability.NewPrometheusMetrics()
	m.IncrCounter("x_total", map[string]string{"module": "a", "tenant": "t1"})
	m.IncrCounter("x_total", map[string]string{"module": "a"}) // tanpa tenant — tidak boleh panic

	_ = scrape(t, m)
}

// TestPrometheusMetrics_NamaSamaJenisBerbeda_TidakPanic membuktikan memakai
// nama metric yang sama untuk dua JENIS berbeda (counter lalu histogram) —
// kesalahan pemanggil yang membuat Prometheus menolak registrasi kedua —
// di-skip dengan aman, bukan meng-crash proses (metrics tak boleh
// menjatuhkan transaksi bisnis pemanggil).
func TestPrometheusMetrics_NamaSamaJenisBerbeda_TidakPanic(t *testing.T) {
	m := observability.NewPrometheusMetrics()
	m.IncrCounter("dup_name", nil)
	m.RecordDuration("dup_name", time.Millisecond, nil) // jenis beda, nama sama — dulu panic

	body := scrape(t, m)
	if !strings.Contains(body, "dup_name") {
		t.Fatalf("counter pertama harus tetap tereskpos, dapat:\n%s", body)
	}
}

// TestPrometheusMetrics_RegistryTerisolasi membuktikan dua instance tidak
// bentrok (registry privat, bukan prometheus.DefaultRegisterer global).
func TestPrometheusMetrics_RegistryTerisolasi(t *testing.T) {
	a := observability.NewPrometheusMetrics()
	b := observability.NewPrometheusMetrics()
	a.IncrCounter("sama_total", nil)
	b.IncrCounter("sama_total", nil)

	bodyA := scrape(t, a)
	bodyB := scrape(t, b)
	if !strings.Contains(bodyA, "sama_total") || !strings.Contains(bodyB, "sama_total") {
		t.Fatalf("kedua instance harus expose metric sendiri-sendiri tanpa error registrasi duplikat")
	}
}
