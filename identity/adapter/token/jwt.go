// Package token adalah driven adapter yang menerbitkan & memverifikasi token internal
// Pamong sebagai JWT HS256. Inilah satu-satunya tempat library JWT (golang-jwt) dan detail
// kriptografi token masuk — domain & use case identity tak pernah menyentuhnya
// (linter: domain-no-infra-import). Codec mengimplementasi port.TokenIssuer & port.TokenVerifier
// sehingga gateway memverifikasi token lewat port tanpa import identity/ (seam, PR-2.4.2).
//
// HS256 dipilih karena token internal diterbitkan & diverifikasi oleh proses yang sama
// (modular monolith); ADR-007. Token SSO eksternal (RS256/JWKS lewat config Auth) adalah
// concern terpisah di PR lain.
package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

const (
	// internalIssuer & internalAudience menamai token internal, TERPISAH dari AuthConfig SSO
	// (JWKSURL/Issuer/Audience = verifikasi token eksternal). Verify mewajibkan keduanya cocok
	// sehingga token dari namespace lain (mis. SSO eksternal) tidak lolos sebagai token internal.
	internalIssuer   = "pamong-identity"
	internalAudience = "pamong-internal"
)

// jwtClaims memetakan port.Claims ke representasi JWT (registered + private claims). Hanya
// hidup di adapter ini; port.Claims tetap library-agnostic. Nama JSON klaim mengikuti
// "Struktur JWT token" di CLAUDE.md.
type jwtClaims struct {
	jwt.RegisteredClaims
	Persona          string   `json:"persona"`
	EmploymentStatus string   `json:"employment_status,omitempty"`
	TenantID         string   `json:"tenant_id,omitempty"`
	CentralRoles     []string `json:"central_roles,omitempty"`
	TenantRoles      []string `json:"tenant_roles,omitempty"`
	TenantScope      []string `json:"tenant_scope,omitempty"`
	IsCrossTenant    bool     `json:"is_cross_tenant,omitempty"`
}

// DefaultMaxBytes adalah pagar ukuran token bawaan bila ops tidak menyetel
// GOV_AUTH_TOKEN_MAX_BYTES. 6 KiB dipilih DI BAWAH batas header nginx yang lazim (8 KiB per
// header pada large_client_header_buffers) dengan sisa ±2 KiB untuk prefiks "Authorization:
// Bearer " dan header lain di request yang sama. Deployment di belakang proxy yang lebih
// longgar (ALB 16 KiB) boleh menaikkannya lewat config; nilainya kebijakan ops, bukan
// konstanta arsitektur (ADR-020).
const DefaultMaxBytes = 6 * 1024

// warnRatio = bagian dari ambang yang, bila terlampaui, membuat token yang MASIH LOLOS ikut
// dicatat sebagai peringatan. 80% dipilih agar peringatan datang saat masih ada ruang bertindak
// (pada ambang default itu ±14 role @25 karakter sebelum penolakan), bukan pada byte terakhir.
const warnRatio = 0.8

// Nama metrik ukuran token. metricTokenBytes adalah histogram byte SEMUA token yang lolos —
// gunanya melihat pertumbuhan (p99 mendekati ambang) SEBELUM ada yang tertolak;
// metricTokenOversize adalah counter penolakan, sinyal yang layak dijadikan alert.
const (
	metricTokenBytes    = "auth_token_bytes"
	metricTokenOversize = "auth_token_oversize_total"
)

// JWTCodec menerbitkan & memverifikasi token internal. Verify berkonsultasi ke
// RevokedTokenStore SETELAH tanda tangan & klaim sah, sehingga jti yang dicabut ditolak.
type JWTCodec struct {
	secret    []byte
	ttl       time.Duration
	maxBytes  int
	warnBytes int
	revoked   domain.RevokedTokenStore
	metrics   port.MetricsPort
	logger    port.Logger
	now       func() time.Time
}

var (
	_ port.TokenIssuer   = (*JWTCodec)(nil)
	_ port.TokenVerifier = (*JWTCodec)(nil)
)

// Options merakit JWTCodec. Sengaja struct, bukan parameter berderet: dependensinya sudah
// enam dan tiga di antaranya opsional, sehingga urutan posisional mudah tertukar tanpa
// terdeteksi compiler (Metrics & Logger keduanya interface).
type Options struct {
	// Secret = kunci HMAC (dari AppConfig; wajib & ≥32 byte di production, ditegakkan
	// config.Validate). TTL = umur token; 0 = 1 jam.
	Secret []byte
	TTL    time.Duration

	// MaxBytes = pagar ukuran token terbit. 0 = DefaultMaxBytes. Tidak ada cara menonaktifkan
	// pagar: ambang yang lebih longgar harus dinyatakan sebagai ANGKA, bukan sebagai "kosong",
	// supaya tak ada jalur di mana pagar hilang karena sebuah field terlewat diisi. Batas atas
	// yang dianggap masuk akal ditegakkan config (config.MaxTokenMaxBytes) — ambang raksasa
	// membatalkan sendiri tujuan pagar lewat MaxHeaderBytes yang diturunkan darinya.
	MaxBytes int

	// Revoked = denylist jti (store DB di runtime, fake di unit test). WAJIB: tanpanya Verify
	// tak bisa memastikan token belum dicabut, dan fail-open di jalur itu tak boleh mungkin.
	Revoked domain.RevokedTokenStore

	// Metrics & Logger = observasi pagar ukuran. Boleh nil (unit test): observasi dibuang,
	// pagar tetap menegakkan. Di produksi keduanya WAJIB diisi — penolakan yang tak ter-log
	// mengulang kegagalan senyap yang jadi alasan pagar ini ada.
	Metrics port.MetricsPort
	Logger  port.Logger
}

// NewJWTCodec membuat codec dari Options. Panic bila Revoked nil: itu kesalahan perakitan yang
// hanya bisa terjadi di composition root (bukan input runtime), dan meloloskannya berarti
// Verify men-dereference nil pada request pertama — gagal di boot jauh lebih murah.
func NewJWTCodec(o Options) *JWTCodec {
	if o.Revoked == nil {
		panic("token.NewJWTCodec: Revoked (RevokedTokenStore) wajib — tanpanya revocation jti tak bisa dicek")
	}
	ttl := o.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	maxBytes := o.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &JWTCodec{
		secret:    o.Secret,
		ttl:       ttl,
		maxBytes:  maxBytes,
		warnBytes: int(float64(maxBytes) * warnRatio),
		revoked:   o.Revoked,
		metrics:   o.Metrics,
		logger:    o.Logger,
		now:       time.Now,
	}
}

// Issue menandatangani token dari Claims. Codec mengisi sendiri klaim infrastruktur
// (jti baru, iat, exp = iat+ttl, iss, aud); klaim identitas berasal dari pemanggil (login).
//
// Di sini juga berdiri PAGAR UKURAN (ADR-020): token yang lebih besar dari maxBytes ditolak,
// tidak diterbitkan. Alasannya bukan kerapian — `central_roles[]` + `tenant_roles[]` adalah
// satu-satunya klaim yang bertumbuh, dan tanpa pagar akun yang mengakumulasi role lintas tahun
// (justru admin tenant) akhirnya menerbitkan token yang DITOLAK PROXY, bukan aplikasi: login
// 200, lalu setiap request berikutnya 400 tanpa satu pun jejak di log Go karena request tak
// pernah sampai. Menolak di sini menukar kegagalan senyap yang tak terdiagnosis dengan satu
// error yang menyebut sebabnya.
func (c *JWTCodec) Issue(ctx context.Context, claims port.Claims) (string, error) {
	now := c.now()
	jti := uuid.New()
	jc := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    internalIssuer,
			Subject:   claims.PersonID.String(),
			Audience:  jwt.ClaimStrings{internalAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(c.ttl)),
			ID:        jti.String(),
		},
		Persona:          claims.Persona,
		EmploymentStatus: claims.EmploymentStatus,
		TenantID:         claims.TenantID,
		CentralRoles:     claims.CentralRoles,
		TenantRoles:      claims.TenantRoles,
		TenantScope:      claims.TenantScope,
		IsCrossTenant:    claims.IsCrossTenant,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jc).SignedString(c.secret)
	if err != nil {
		// HMAC signing praktis tak pernah gagal; bila terjadi, ini kesalahan internal (500).
		return "", fmt.Errorf("menandatangani token: %w", err)
	}
	// Diukur SESUDAH ditandatangani, pada artefak yang benar-benar dikirim: base64url +
	// header + tanda tangan membuat ukuran akhir tak bisa disimpulkan dari panjang klaim.
	if size := len(signed); size > c.maxBytes {
		return "", c.rejectOversize(ctx, claims, size)
	}
	c.observeSize(ctx, claims, len(signed))
	return signed, nil
}

// rejectOversize mencatat penolakan (log + metrik) lalu mengembalikan error yang MENYEBUT
// angkanya. Pesan sengaja menyertakan jumlah role dan ambangnya: penerimanya adalah orang yang
// baru saja gagal login dan admin yang membaca log, dan keduanya butuh tahu bahwa yang harus
// dikurangi adalah ROLE — bukan mencoba lagi. Aktor sudah terotentikasi pada titik ini
// (kredensial terverifikasi sebelum token dicetak), jadi angka ini bukan kebocoran ke anonim.
func (c *JWTCodec) rejectOversize(ctx context.Context, claims port.Claims, size int) error {
	nCentral, nTenant := len(claims.CentralRoles), len(claims.TenantRoles)
	if c.metrics != nil {
		c.metrics.IncrCounter(metricTokenOversize, map[string]string{"persona": claims.Persona})
	}
	if c.logger != nil {
		// person_id & tenant_id dicatat agar admin bisa menemukan akunnya; token sendiri
		// TIDAK PERNAH ikut ter-log (ia kredensial, sekalipun tak terpakai).
		c.logger.Error(ctx, "token melewati pagar ukuran; login ditolak",
			port.F("person_id", claims.PersonID.String()),
			port.F("tenant_id", claims.TenantID),
			port.F("token_bytes", size),
			port.F("max_bytes", c.maxBytes),
			port.F("central_roles", nCentral),
			port.F("tenant_roles", nTenant),
		)
	}
	return core.ErrTokenTooLarge(fmt.Sprintf(
		"token %d byte melewati batas %d byte dengan %d role sentral + %d role tenant; "+
			"kurangi role yang dipegang akun ini, atau naikkan GOV_AUTH_TOKEN_MAX_BYTES bila "+
			"proxy di depan aplikasi memang mengizinkan header lebih besar",
		size, c.maxBytes, nCentral, nTenant))
}

// observeSize mencatat ukuran token yang LOLOS: ke histogram byte, dan — bila sudah melewati
// warnRatio dari ambang — ke log sebagai peringatan dini.
//
// Peringatan dini itu bukan hiasan. Pagar yang hanya menolak akan MENGUNCI akun yang tadi masih
// bekerja, pada saat rilis, tanpa siapa pun pernah tahu ia mendekati batas. Histogram menjawab itu
// hanya di deployment yang scrape-nya sudah jalan (mount `GET /metrics` = PR-W6); log bekerja hari
// ini di semua deployment. Volumenya terjaga oleh konstruksi: hanya akun yang benar-benar mendekati
// batas yang menghasilkan baris ini, dan justru akun itulah yang perlu dilihat operator.
//
// Tag metrik: persona saja — sengaja tanpa person/tenant_id (high-cardinality).
func (c *JWTCodec) observeSize(ctx context.Context, claims port.Claims, size int) {
	if c.metrics != nil {
		c.metrics.RecordSize(metricTokenBytes, size, map[string]string{"persona": claims.Persona})
	}
	if c.logger == nil || size < c.warnBytes {
		return
	}
	c.logger.Warn(ctx, "ukuran token mendekati pagar; akun ini akan terkunci bila role bertambah",
		port.F("person_id", claims.PersonID.String()),
		port.F("tenant_id", claims.TenantID),
		port.F("token_bytes", size),
		port.F("max_bytes", c.maxBytes),
		port.F("central_roles", len(claims.CentralRoles)),
		port.F("tenant_roles", len(claims.TenantRoles)),
	)
}

// Verify memvalidasi tanda tangan, masa berlaku, issuer/audience, lalu memastikan jti belum
// dicabut. Kegagalan otentikasi (tanda tangan/format/kedaluwarsa/scope/revoked) → 401
// (core.ErrUnauthorized). Kegagalan store revocation → error internal (fail-closed: request ditolak).
func (c *JWTCodec) Verify(ctx context.Context, raw string) (*port.Claims, error) {
	parser := jwt.NewParser(
		// Pin algoritma → tolak alg=none & alg-confusion (RS256 dipalsukan jadi HMAC).
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(internalIssuer),
		jwt.WithAudience(internalAudience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(c.now),
	)
	var jc jwtClaims
	_, err := parser.ParseWithClaims(raw, &jc, func(t *jwt.Token) (any, error) {
		// Pertahanan berlapis selain WithValidMethods: pastikan metode benar-benar HMAC.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("metode tanda tangan tak terduga: %v", t.Header["alg"])
		}
		return c.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, core.ErrUnauthorized("token kedaluwarsa")
		}
		return nil, core.ErrUnauthorized("token tidak valid")
	}

	personID, err := uuid.Parse(jc.Subject)
	if err != nil {
		return nil, core.ErrUnauthorized("token tidak valid")
	}
	jti, err := uuid.Parse(jc.ID)
	if err != nil {
		return nil, core.ErrUnauthorized("token tidak valid")
	}

	revoked, err := c.revoked.IsRevoked(ctx, jti)
	if err != nil {
		// Tak bisa memastikan status revocation → tolak (fail-closed), bukan lolos.
		return nil, fmt.Errorf("cek revocation: %w", err)
	}
	if revoked {
		return nil, core.ErrUnauthorized("token telah dicabut")
	}

	return &port.Claims{
		PersonID:         personID,
		Persona:          jc.Persona,
		EmploymentStatus: jc.EmploymentStatus,
		TenantID:         jc.TenantID,
		CentralRoles:     jc.CentralRoles,
		TenantRoles:      jc.TenantRoles,
		TenantScope:      jc.TenantScope,
		IsCrossTenant:    jc.IsCrossTenant,
		TokenID:          jc.ID,
		IssuedAt:         jc.IssuedAt.Time,
		ExpiresAt:        jc.ExpiresAt.Time,
	}, nil
}
