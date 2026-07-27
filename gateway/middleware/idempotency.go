package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// maxReplayBody membatasi ukuran body respons yang di-cache untuk replay. Respons lebih besar
// tetap dikirim ke klien (write-through) tapi TIDAK di-cache — retry berikutnya mengeksekusi
// ulang (kehilangan jaminan idempotency untuk respons besar, bukan kebenaran).
const maxReplayBody = 1 << 20 // 1 MiB

// Idempotency menegakkan idempotency request MUTASI ber-Idempotency-Key (PRD gateway F3
// langkah 6 + acceptance: "request mutasi duplikat (key sama) → response sama tanpa efek
// ganda"). Framework yang menangani ini — bukan use case (CLAUDE.md §Data integrity).
//
// Alur:
//   - Bukan metode mutasi (GET/HEAD/…) atau tak ada header Idempotency-Key → lewati (opt-in
//     oleh caller, sesuai CLAUDE.md "use case menerima idempotency key DARI CALLER").
//   - Citizen tanpa tenant (TenantID kosong) → lewati: store hidup di tenant DB. Idempotency
//     mutasi citizen lewat store sentral = DEFERRED(Phase-5.1.x).
//   - Reserve klaim atomik. Bila sudah ada entri untuk (principal, key):
//   - fingerprint beda (key dipakai untuk request berbeda) → 422.
//   - belum selesai (kembar in-flight) → 409.
//   - sudah selesai → REPLAY status+body tersimpan (tak menjalankan handler → tanpa efek ganda).
//   - Reservasi baru → jalankan handler sambil merekam respons; 2xx → Complete (simpan untuk
//     replay), selain itu → Release (reservasi dilepas agar retry boleh mengeksekusi ulang).
//
// Fail-closed: bila store error saat Reserve, request DITOLAK 503 (bukan diproses tanpa
// jaminan) — klien boleh retry dengan aman karena handler belum berjalan.
//
// Dipasang SETELAH RateLimit (PRD urutan 5→6) & SETELAH Auth/TenantResolver (butuh principal
// + tenant). Key di-scope ke principal di store (person_id + key), mencegah satu user
// membaca/menimpa respons user lain lewat nilai key yang sama.
func Idempotency(store port.IdempotencyStore, logger port.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !isMutation(r.Method) || key == "" {
				next.ServeHTTP(w, r)
				return
			}
			c := gateway.FromRequest(r)
			tenant := c.TenantID()
			if tenant == "" {
				next.ServeHTTP(w, r)
				return
			}
			person := c.PersonID()

			// Baca & buffer body agar bisa mem-fingerprint DAN handler tetap bisa membacanya.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				gateway.WriteError(w, gateway.ErrBadRequest("gagal membaca body request"))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			fingerprint := requestFingerprint(r.Method, r.URL.Path, body)

			rec, reserved, err := store.Reserve(r.Context(), tenant, person, key, fingerprint)
			if err != nil {
				// Fail-closed: proteksi tak pasti → tolak (retryable).
				gateway.WriteError(w, core.ErrUnavailable("layanan idempotency tidak tersedia"))
				return
			}

			if !reserved {
				switch {
				case rec.Fingerprint != fingerprint:
					gateway.WriteError(w, core.ErrValidation("Idempotency-Key",
						"idempotency key sudah dipakai untuk request yang berbeda"))
				case !rec.Completed:
					gateway.WriteError(w, core.ErrConflict(
						"request dengan idempotency key ini sedang diproses"))
				default:
					replayResponse(w, rec)
				}
				return
			}

			// Reservasi baru → jalankan handler sambil merekam respons.
			cw := &captureWriter{ResponseWriter: w, max: maxReplayBody}
			finalized := false
			defer func() {
				// Panic / unwind sebelum finalisasi: lepaskan reservasi agar tak menyandera key
				// (best-effort; pendingTTL pendek jadi backstop). Panic lanjut ke Recovery.
				if !finalized {
					if err := store.Release(r.Context(), tenant, person, key); err != nil {
						logger.Warn(r.Context(), "idempotency: gagal melepas reservasi setelah panic",
							port.F("error", err.Error()))
					}
				}
			}()

			next.ServeHTTP(cw, r)
			finalized = true

			status := cw.status
			if status == 0 {
				status = http.StatusOK // handler yang menulis tanpa WriteHeader → 200 implisit
			}
			if status >= 200 && status < 300 && !cw.truncated {
				if err := store.Complete(r.Context(), tenant, person, key, status, cw.buf.Bytes()); err != nil {
					// Best-effort: respons sudah terkirim ke klien. Entri tetap pending → retry
					// mendapat 409 sampai pendingTTL habis; tak ada eksekusi ganda.
					logger.Warn(r.Context(), "idempotency: gagal menyimpan respons",
						port.F("error", err.Error()))
				}
			} else {
				if err := store.Release(r.Context(), tenant, person, key); err != nil {
					logger.Warn(r.Context(), "idempotency: gagal melepas reservasi setelah respons non-2xx",
						port.F("error", err.Error()))
				}
			}
		})
	}
}

// isMutation melaporkan apakah metode HTTP memutasi state (perlu proteksi idempotency).
func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// requestFingerprint meng-hash identitas request (metode + path + body) untuk mendeteksi
// idempotency key yang dipakai-ulang untuk request yang BERBEDA.
func requestFingerprint(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte("\n"))
	h.Write([]byte(path))
	h.Write([]byte("\n"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// replayResponse menulis respons tersimpan tanpa menjalankan handler. Hanya status + body
// (JSON) yang di-replay; header sewenang-wenang tidak disimpan (respons framework = JSON).
// Header Idempotent-Replay menandai bahwa ini replay (observability).
func replayResponse(w http.ResponseWriter, rec *port.IdempotencyRecord) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(rec.Status)
	_, _ = w.Write(rec.Body)
}

// captureWriter meneruskan (write-through) respons ke klien sambil merekamnya untuk disimpan.
// Merekam sampai max byte; bila terlampaui, body rekaman dibuang & truncated diset (tak
// di-cache) — write-through ke klien tetap berjalan penuh.
type captureWriter struct {
	http.ResponseWriter
	status    int
	buf       bytes.Buffer
	max       int
	truncated bool
	wrote     bool
}

func (c *captureWriter) WriteHeader(status int) {
	if c.wrote {
		return
	}
	c.status = status
	c.wrote = true
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if !c.truncated {
		if c.buf.Len()+len(b) > c.max {
			c.truncated = true
			c.buf.Reset() // buang rekaman parsial; jangan pernah replay body tak lengkap
		} else {
			c.buf.Write(b)
		}
	}
	return c.ResponseWriter.Write(b)
}
