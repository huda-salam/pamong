package middleware_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// memStore adalah port.IdempotencyStore in-memory dengan semantik yang sama dengan store DB
// (reservasi atomik + scope per-principal), cukup untuk menguji middleware tanpa DB.
type memStore struct {
	mu         sync.Mutex
	m          map[string]*memRec
	reserveErr error
}

type memRec struct {
	fingerprint string
	status      int
	body        []byte
	completed   bool
	expires     time.Time
}

func newMemStore() *memStore { return &memStore{m: make(map[string]*memRec)} }

func memKey(person uuid.UUID, key string) string { return person.String() + "|" + key }

func (s *memStore) Reserve(_ context.Context, _ string, person uuid.UUID, key, fp string) (*port.IdempotencyRecord, bool, error) {
	if s.reserveErr != nil {
		return nil, false, s.reserveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kk := memKey(person, key)
	if r, ok := s.m[kk]; ok && time.Now().Before(r.expires) {
		return &port.IdempotencyRecord{Fingerprint: r.fingerprint, Status: r.status, Body: r.body, Completed: r.completed}, false, nil
	}
	s.m[kk] = &memRec{fingerprint: fp, expires: time.Now().Add(2 * time.Minute)}
	return nil, true, nil
}

func (s *memStore) Complete(_ context.Context, _ string, person uuid.UUID, key string, status int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.m[memKey(person, key)]; ok {
		r.status = status
		r.body = append([]byte(nil), body...)
		r.completed = true
		r.expires = time.Now().Add(24 * time.Hour)
	}
	return nil
}

func (s *memStore) Release(_ context.Context, _ string, person uuid.UUID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kk := memKey(person, key)
	if r, ok := s.m[kk]; ok && !r.completed {
		delete(s.m, kk)
	}
	return nil
}

// idemReq membuat request mutasi ber-Context principal (person tetap agar scope key cocok).
func idemReq(method, path string, body []byte, key, tenant string, person uuid.UUID) *http.Request {
	c := gateway.NewContextFromClaims(context.Background(), &port.Claims{
		PersonID: person, Persona: "employee", TenantID: tenant,
	})
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return gateway.WithContext(r, c)
}

// fingerprintOf mereplikasi perhitungan fingerprint middleware (sha256 method+path+body),
// dipakai test in-flight untuk menyetel reservasi pending dengan fingerprint yang cocok.
func fingerprintOf(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte("\n"))
	h.Write([]byte(path))
	h.Write([]byte("\n"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// countingHandler membalas status+body tetap & menghitung berapa kali ia benar-benar jalan.
func countingHandler(runs *int, status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*runs++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func TestIdempotency_ReplayRequestDuplikat(t *testing.T) {
	store := newMemStore()
	person := uuid.New()
	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusCreated, `{"id":"abc"}`))

	// Request pertama: handler jalan, respons 201 tersimpan.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, idemReq(http.MethodPost, "/surat", []byte(`{"perihal":"x"}`), "key-1", "pemkot-a", person))
	if w1.Code != http.StatusCreated || w1.Body.String() != `{"id":"abc"}` {
		t.Fatalf("req#1 = %d %q, mau 201 {\"id\":\"abc\"}", w1.Code, w1.Body.String())
	}
	if runs != 1 {
		t.Fatalf("handler harus jalan 1x, got %d", runs)
	}

	// Request kedua identik (key sama): REPLAY — handler TIDAK jalan lagi, respons sama.
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, idemReq(http.MethodPost, "/surat", []byte(`{"perihal":"x"}`), "key-1", "pemkot-a", person))
	if w2.Code != http.StatusCreated || w2.Body.String() != `{"id":"abc"}` {
		t.Fatalf("req#2 (replay) = %d %q, mau 201 {\"id\":\"abc\"}", w2.Code, w2.Body.String())
	}
	if runs != 1 {
		t.Fatalf("replay tak boleh menjalankan handler lagi; runs=%d (efek ganda!)", runs)
	}
	if w2.Header().Get("Idempotent-Replay") != "true" {
		t.Error("replay harus menandai header Idempotent-Replay: true")
	}
}

func TestIdempotency_FingerprintBeda_422(t *testing.T) {
	store := newMemStore()
	person := uuid.New()
	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusCreated, `{}`))

	h.ServeHTTP(httptest.NewRecorder(), idemReq(http.MethodPost, "/surat", []byte(`{"a":1}`), "key-1", "pemkot-a", person))

	// Key sama tapi body berbeda → key dipakai-ulang untuk request berbeda → 422.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, idemReq(http.MethodPost, "/surat", []byte(`{"a":2}`), "key-1", "pemkot-a", person))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("fingerprint beda harus 422, got %d", w.Code)
	}
	if runs != 1 {
		t.Fatalf("request kedua tak boleh menjalankan handler; runs=%d", runs)
	}
}

func TestIdempotency_InFlight_409(t *testing.T) {
	store := newMemStore()
	person := uuid.New()
	// Sisipkan reservasi pending (belum completed) untuk mensimulasikan request kembar in-flight.
	store.m[memKey(person, "key-1")] = &memRec{fingerprint: "", completed: false, expires: time.Now().Add(time.Minute)}

	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusOK, `{}`))

	// Body kosong → fingerprint POST/\n/surat/\n(kosong); reservasi pending fingerprint "" ≠,
	// tapi in-flight (belum completed) diperiksa hanya bila fingerprint cocok. Pakai fingerprint
	// cocok: set fingerprint reservasi = fingerprint request kosong.
	store.m[memKey(person, "key-1")].fingerprint = fingerprintOf(http.MethodPost, "/surat", nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, idemReq(http.MethodPost, "/surat", nil, "key-1", "pemkot-a", person))
	if w.Code != http.StatusConflict {
		t.Fatalf("kembar in-flight harus 409, got %d", w.Code)
	}
	if runs != 0 {
		t.Fatalf("in-flight tak boleh menjalankan handler; runs=%d", runs)
	}
}

func TestIdempotency_StoreError_FailClosed_503(t *testing.T) {
	store := newMemStore()
	store.reserveErr = errors.New("db down")
	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusOK, `{}`))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, idemReq(http.MethodPost, "/surat", []byte(`{}`), "key-1", "pemkot-a", uuid.New()))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error harus fail-closed 503, got %d", w.Code)
	}
	if runs != 0 {
		t.Fatalf("fail-closed: handler tak boleh jalan; runs=%d", runs)
	}
}

func TestIdempotency_ResponsNon2xx_Dilepas(t *testing.T) {
	store := newMemStore()
	person := uuid.New()
	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusBadRequest, `{"error":"x"}`))

	// Request gagal (400) → reservasi dilepas → retry mengeksekusi ulang (bukan replay 400).
	h.ServeHTTP(httptest.NewRecorder(), idemReq(http.MethodPost, "/surat", []byte(`{}`), "key-1", "pemkot-a", person))
	h.ServeHTTP(httptest.NewRecorder(), idemReq(http.MethodPost, "/surat", []byte(`{}`), "key-1", "pemkot-a", person))
	if runs != 2 {
		t.Fatalf("respons non-2xx harus dilepas → handler jalan lagi saat retry; runs=%d (mau 2)", runs)
	}
}

func TestIdempotency_LewatiNonMutasiDanTanpaKey(t *testing.T) {
	store := newMemStore()

	t.Run("GET (non-mutasi) dengan key → lewati", func(t *testing.T) {
		var runs int
		h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusOK, `{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, idemReq(http.MethodGet, "/surat", nil, "key-1", "pemkot-a", uuid.New()))
		if w.Code != http.StatusOK || runs != 1 {
			t.Fatalf("GET harus lewati idempotency (handler jalan); code=%d runs=%d", w.Code, runs)
		}
	})

	t.Run("POST tanpa Idempotency-Key → lewati", func(t *testing.T) {
		var runs int
		h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusCreated, `{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, idemReq(http.MethodPost, "/surat", []byte(`{}`), "", "pemkot-a", uuid.New()))
		if w.Code != http.StatusCreated || runs != 1 {
			t.Fatalf("POST tanpa key harus lewati; code=%d runs=%d", w.Code, runs)
		}
	})

	t.Run("citizen tanpa tenant → lewati", func(t *testing.T) {
		var runs int
		h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusCreated, `{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, idemReq(http.MethodPost, "/lapor", []byte(`{}`), "key-x", "", uuid.New()))
		if w.Code != http.StatusCreated || runs != 1 {
			t.Fatalf("citizen tanpa tenant harus lewati; code=%d runs=%d", w.Code, runs)
		}
	})
}

func TestIdempotency_ResponsBesar_TidakDiCache(t *testing.T) {
	store := newMemStore()
	person := uuid.New()
	big := strings.Repeat("x", (1<<20)+10) // > maxReplayBody (1 MiB)
	var runs int
	h := middleware.Idempotency(store, testkit.NewNoopLogger())(countingHandler(&runs, http.StatusOK, big))

	h.ServeHTTP(httptest.NewRecorder(), idemReq(http.MethodPost, "/export", []byte(`{}`), "key-1", "pemkot-a", person))
	// Respons > cap tidak di-cache → retry mengeksekusi ulang (bukan replay terpotong).
	h.ServeHTTP(httptest.NewRecorder(), idemReq(http.MethodPost, "/export", []byte(`{}`), "key-1", "pemkot-a", person))
	if runs != 2 {
		t.Fatalf("respons besar tak boleh di-cache → handler jalan lagi; runs=%d (mau 2)", runs)
	}
}
