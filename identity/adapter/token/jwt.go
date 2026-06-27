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

// JWTCodec menerbitkan & memverifikasi token internal. Verify berkonsultasi ke
// RevokedTokenStore SETELAH tanda tangan & klaim sah, sehingga jti yang dicabut ditolak.
type JWTCodec struct {
	secret  []byte
	ttl     time.Duration
	revoked domain.RevokedTokenStore
	now     func() time.Time
}

var (
	_ port.TokenIssuer   = (*JWTCodec)(nil)
	_ port.TokenVerifier = (*JWTCodec)(nil)
)

// NewJWTCodec membuat codec. secret = kunci HMAC (dari AppConfig; wajib & ≥32 byte di
// production, ditegakkan config.Validate). ttl = umur token. revoked = denylist jti (store DB
// di runtime, atau fake di unit test).
func NewJWTCodec(secret []byte, ttl time.Duration, revoked domain.RevokedTokenStore) *JWTCodec {
	return &JWTCodec{secret: secret, ttl: ttl, revoked: revoked, now: time.Now}
}

// Issue menandatangani token dari Claims. Codec mengisi sendiri klaim infrastruktur
// (jti baru, iat, exp = iat+ttl, iss, aud); klaim identitas berasal dari pemanggil (login).
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
	return signed, nil
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
