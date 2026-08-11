package token

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// --- perkakas observasi: metrics & logger perekam (lokal, seperti fakeRevoked) ---

type sizeObs struct {
	name, persona string
	bytes         int
}

type recordingMetrics struct {
	mu       sync.Mutex
	sizes    []sizeObs
	counters []string
}

var _ port.MetricsPort = (*recordingMetrics)(nil)

func (m *recordingMetrics) RecordSize(name string, bytes int, tags map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sizes = append(m.sizes, sizeObs{name: name, persona: tags["persona"], bytes: bytes})
}

func (m *recordingMetrics) IncrCounter(name string, _ map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = append(m.counters, name)
}

func (m *recordingMetrics) RecordDuration(string, time.Duration, map[string]string) {}
func (m *recordingMetrics) SetGauge(string, float64, map[string]string)             {}

type logLine struct {
	msg    string
	fields map[string]any
}

type recordingLogger struct {
	mu    sync.Mutex
	errs  []logLine
	warns []logLine
	extra []port.Field
}

var _ port.Logger = (*recordingLogger)(nil)

func (l *recordingLogger) line(msg string, fields []port.Field) logLine {
	m := map[string]any{}
	for _, f := range append(l.extra, fields...) {
		m[f.Key] = f.Value
	}
	return logLine{msg: msg, fields: m}
}

func (l *recordingLogger) Error(_ context.Context, msg string, fields ...port.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, l.line(msg, fields))
}

func (l *recordingLogger) Warn(_ context.Context, msg string, fields ...port.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, l.line(msg, fields))
}

func (l *recordingLogger) Debug(context.Context, string, ...port.Field) {}
func (l *recordingLogger) Info(context.Context, string, ...port.Field)  {}
func (l *recordingLogger) With(f ...port.Field) port.Logger {
	return &recordingLogger{extra: append(l.extra, f...)}
}

// rolesOfLength membuat n nama role sepanjang length karakter — bentuk yang PERSIS diizinkan
// tenantRoleNameRe hari ini (a-z0-9_ , tanpa batas panjang), bukan string acak.
func rolesOfLength(n, length int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		out = append(out, strings.Repeat("r", length-len(suffix))+suffix)
	}
	return out
}

func assertTokenTooLarge(t *testing.T, err error) *core.FrameworkError {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) {
		t.Fatalf("expect *core.FrameworkError, got %T: %v", err, err)
	}
	if fe.Code != "TOKEN_TOO_LARGE" {
		t.Fatalf("expect code TOKEN_TOO_LARGE, got %q (%s)", fe.Code, fe.Message)
	}
	return fe
}

// TestJWTCodec_Issue_DibawahAmbang_Lolos — pagar tidak boleh mengganggu kasus normal. 50 role
// @25 karakter adalah bentuk akun tenant yang wajar dan harus tetap terbit.
func TestJWTCodec_Issue_DibawahAmbang_Lolos(t *testing.T) {
	met := &recordingMetrics{}
	c := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, Metrics: met})

	claims := sampleClaims()
	claims.TenantRoles = rolesOfLength(50, 25)
	raw, err := c.Issue(context.Background(), claims)
	if err != nil {
		t.Fatalf("50 role@25 char harus lolos pagar default (%d byte): %v", DefaultMaxBytes, err)
	}
	if len(raw) > DefaultMaxBytes {
		t.Fatalf("token %d byte lolos padahal di atas ambang %d — pagar tidak menegakkan", len(raw), DefaultMaxBytes)
	}

	// Token yang lolos WAJIB terobservasi: itu satu-satunya cara pertumbuhan terlihat sebelum
	// ada penolakan pertama.
	if len(met.sizes) != 1 {
		t.Fatalf("mau 1 observasi ukuran, dapat %d", len(met.sizes))
	}
	got := met.sizes[0]
	if got.name != metricTokenBytes || got.bytes != len(raw) || got.persona != claims.Persona {
		t.Fatalf("observasi ukuran salah: %+v (mau name=%q bytes=%d persona=%q)",
			got, metricTokenBytes, len(raw), claims.Persona)
	}
	if len(met.counters) != 0 {
		t.Fatalf("token yang lolos tidak boleh menaikkan counter penolakan: %v", met.counters)
	}
}

// TestJWTCodec_Issue_DiatasAmbang_Ditolak — DoD PR-W3c: token yang melewati ambang TIDAK
// diterbitkan (string kosong), errornya bertipe & MENYEBUT jumlah role, penolakannya ter-log
// dan tercatat sebagai metrik. Ukuran akun sengaja diambil dari pengukuran nyata di ROADMAP:
// 100 role @100 karakter ≈ 14 KB, jauh di atas ambang default 6 KB.
func TestJWTCodec_Issue_DiatasAmbang_Ditolak(t *testing.T) {
	met := &recordingMetrics{}
	log := &recordingLogger{}
	c := NewJWTCodec(Options{
		Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, Metrics: met, Logger: log,
	})

	claims := sampleClaims()
	claims.CentralRoles = rolesOfLength(3, 100)
	claims.TenantRoles = rolesOfLength(100, 100)

	raw, err := c.Issue(context.Background(), claims)
	if err == nil {
		t.Fatalf("token %d byte diterbitkan padahal ambangnya %d — pagar tidak menegakkan", len(raw), DefaultMaxBytes)
	}
	if raw != "" {
		t.Fatalf("token bocor ke pemanggil saat ditolak (%d byte); harus string kosong", len(raw))
	}
	fe := assertTokenTooLarge(t, err)
	// Pesan harus menuntun ke tindakan: jumlah role & ambangnya, bukan "kesalahan internal".
	for _, want := range []string{"3 role sentral", "100 role tenant", strconv.Itoa(DefaultMaxBytes)} {
		if !strings.Contains(fe.Message, want) {
			t.Fatalf("pesan error tak menyebut %q: %s", want, fe.Message)
		}
	}

	if len(met.counters) != 1 || met.counters[0] != metricTokenOversize {
		t.Fatalf("mau 1 counter %q, dapat %v", metricTokenOversize, met.counters)
	}
	if len(met.sizes) != 0 {
		t.Fatalf("token yang DITOLAK tidak boleh masuk histogram token terbit: %+v", met.sizes)
	}
	if len(log.errs) != 1 {
		t.Fatalf("mau 1 log error penolakan, dapat %d", len(log.errs))
	}
	line := log.errs[0]
	if line.fields["person_id"] != claims.PersonID.String() {
		t.Fatalf("log tanpa person_id yang bisa ditelusuri admin: %+v", line.fields)
	}
	if line.fields["central_roles"] != 3 || line.fields["tenant_roles"] != 100 {
		t.Fatalf("log tak memuat jumlah role: %+v", line.fields)
	}
	// Token adalah kredensial sekalipun tak terpakai — ia tak boleh pernah ter-log.
	for k, v := range line.fields {
		if s, ok := v.(string); ok && strings.Count(s, ".") == 2 && len(s) > 100 {
			t.Fatalf("field log %q tampak berisi token utuh: %s", k, s)
		}
	}
}

// TestJWTCodec_Issue_AmbangKustom_Dipakai — ambang adalah kebijakan ops; nilai dari config
// harus benar-benar yang menegakkan, bukan hanya tersimpan.
func TestJWTCodec_Issue_AmbangKustom_Dipakai(t *testing.T) {
	claims := sampleClaims()
	base := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}})
	raw, err := base.Issue(context.Background(), claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	size := len(raw)

	// Satu byte di bawah ukuran token yang sama → ditolak.
	tight := NewJWTCodec(Options{
		Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, MaxBytes: size - 1,
	})
	if _, err := tight.Issue(context.Background(), claims); err == nil {
		t.Fatalf("ambang %d byte harus menolak token %d byte", size-1, size)
	} else {
		assertTokenTooLarge(t, err)
	}

	// Tepat sebesar tokennya → lolos (perbandingan ">" bukan ">=", batas inklusif).
	exact := NewJWTCodec(Options{
		Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, MaxBytes: size,
	})
	if _, err := exact.Issue(context.Background(), claims); err != nil {
		t.Fatalf("ambang %d byte harus meloloskan token %d byte: %v", size, size, err)
	}
}

// TestJWTCodec_Issue_AmbangNol_PakaiDefault — Options tanpa MaxBytes tidak boleh berarti "tanpa
// pagar": deployment yang lupa menyetelnya justru yang paling butuh dilindungi.
func TestJWTCodec_Issue_AmbangNol_PakaiDefault(t *testing.T) {
	c := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}})
	if c.maxBytes != DefaultMaxBytes {
		t.Fatalf("maxBytes = %d, mau default %d", c.maxBytes, DefaultMaxBytes)
	}
	claims := sampleClaims()
	claims.TenantRoles = rolesOfLength(100, 100)
	if _, err := c.Issue(context.Background(), claims); err == nil {
		t.Fatal("Options tanpa MaxBytes meloloskan token 14 KB — pagar hilang saat field terlewat diisi")
	}
}

// TestJWTCodec_Issue_TanpaObservasi_TetapMenegakkan — Metrics/Logger nil (unit test) tak boleh
// membuat pagar diam-diam mati, dan tak boleh panic.
func TestJWTCodec_Issue_TanpaObservasi_TetapMenegakkan(t *testing.T) {
	c := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, MaxBytes: 1024})
	claims := sampleClaims()
	claims.TenantRoles = rolesOfLength(20, 50)
	if _, err := c.Issue(context.Background(), claims); err == nil {
		t.Fatal("pagar tidak menegakkan saat Metrics & Logger nil")
	} else {
		assertTokenTooLarge(t, err)
	}
}

// TestNewJWTCodec_RevokedNil_Panic — kesalahan perakitan gagal di boot, bukan pada request
// pertama (nil deref di Verify).
func TestNewJWTCodec_RevokedNil_Panic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewJWTCodec tanpa Revoked harus panic")
		}
	}()
	NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour})
}

// TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole mendokumentasikan ANGKA yang menjadi alasan
// pagar ini ada (ROADMAP PR-W3c: dasar ~420 B, tiap role ≈ panjang × 1,37). Ia gagal bila
// biaya per-role berubah drastis — mis. karena ada yang memasukkan permission ke token, cara
// paling umum JWT membengkak.
func TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole(t *testing.T) {
	c := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, MaxBytes: 1 << 20})
	measure := func(n, length int) int {
		claims := port.Claims{PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-surabaya"}
		claims.TenantRoles = rolesOfLength(n, length)
		raw, err := c.Issue(context.Background(), claims)
		if err != nil {
			t.Fatalf("Issue(%d role@%d): %v", n, length, err)
		}
		return len(raw)
	}

	base := measure(0, 0)
	if base > 600 {
		t.Fatalf("token dasar %d byte, jauh di atas 383 byte yang diukur — ada klaim baru yang "+
			"bertumbuh; tinjau ulang ambang default %d", base, DefaultMaxBytes)
	}
	perRole := float64(measure(100, 100)-base) / 100.0
	if perRole > 100*1.6 {
		t.Fatalf("biaya per role = %.1f byte untuk nama 100 char (harapan ≈137). Kenaikan besar "+
			"biasanya berarti ada muatan baru per role di token (mis. permission) — itu ditolak "+
			"desain ADR-020, satu role tetap satu entri", perRole)
	}
	if got, want := measure(100, 100), 8*1024; got < want {
		t.Fatalf("100 role@100 char = %d byte; test ini mengasumsikan ia melewati batas nginx "+
			"8 KiB (%d) — bila tidak lagi, angka di ROADMAP/ADR-020 perlu diperbarui", got, want)
	}
	t.Logf("dasar=%d byte, per-role(100 char)=%.1f byte, 100 role=%d byte",
		base, perRole, measure(100, 100))
}

// TestJWTCodec_Issue_MendekatiAmbang_Diperingatkan: pagar yang HANYA menolak akan mengunci akun
// yang tadi masih bekerja, tepat saat rilis, tanpa siapa pun pernah tahu ia mendekati batas.
// Karena itu token yang masih LOLOS tapi sudah melewati warnRatio harus meninggalkan jejak —
// dan jejaknya harus di jalur yang bekerja hari ini (log), bukan hanya histogram yang menunggu
// endpoint /metrics ter-mount (PR-W6).
func TestJWTCodec_Issue_MendekatiAmbang_Diperingatkan(t *testing.T) {
	log := &recordingLogger{}
	claims := sampleClaims()

	probe := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}})
	raw, err := probe.Issue(context.Background(), claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Ambang yang membuat token ini duduk di ~90% — lolos, tapi layak diperingatkan.
	c := NewJWTCodec(Options{
		Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, Logger: log,
		MaxBytes: int(float64(len(raw)) / 0.9),
	})
	if _, err := c.Issue(context.Background(), claims); err != nil {
		t.Fatalf("token di bawah ambang harus tetap terbit: %v", err)
	}

	if len(log.warns) != 1 {
		t.Fatalf("mau 1 peringatan dini, dapat %d", len(log.warns))
	}
	if len(log.errs) != 0 {
		t.Fatalf("token yang LOLOS tak boleh menghasilkan log error: %+v", log.errs)
	}
	w := log.warns[0]
	if w.fields["person_id"] != claims.PersonID.String() || w.fields["token_bytes"] != len(raw) {
		t.Fatalf("peringatan tak memuat akun & ukurannya: %+v", w.fields)
	}
}

// TestJWTCodec_Issue_JauhDibawahAmbang_TanpaPeringatan: peringatan dini hanya berguna bila ia
// jarang. Token normal tidak boleh menghasilkan satu baris log per login.
func TestJWTCodec_Issue_JauhDibawahAmbang_TanpaPeringatan(t *testing.T) {
	log := &recordingLogger{}
	c := NewJWTCodec(Options{Secret: testSecret, TTL: time.Hour, Revoked: &fakeRevoked{}, Logger: log})

	if _, err := c.Issue(context.Background(), sampleClaims()); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(log.warns) != 0 || len(log.errs) != 0 {
		t.Fatalf("token normal harus SENYAP: warns=%+v errs=%+v", log.warns, log.errs)
	}
}
