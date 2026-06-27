package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Claims adalah muatan token internal yang diterbitkan identity dan dibaca gateway.
// Ia library-agnostic (tanpa tipe JWT) — sama seperti TenantInfo: kontrak lintas-layer
// di port/ sehingga gateway dapat mengonsumsi hasil verify tanpa import identity/ maupun
// library JWT apa pun. Codec konkret (identity/adapter/token) yang memetakan Claims ini
// ke/dari representasi JWT.
//
// Pembagian pengisian (PR-2.4.1):
//   - Klaim infrastruktur (TokenID/jti, IssuedAt/iat, ExpiresAt/exp, plus iss/aud yang
//     internal ke codec) diisi codec saat Issue.
//   - Klaim identitas (PersonID, Persona, ... IsCrossTenant) diisi PEMANGGIL — alur login
//     (PR-2.4.3/2.4.4) yang me-resolve person, role, dan tenant. Di PR-2.4.1 hanya fixture test.
type Claims struct {
	PersonID         uuid.UUID // sub
	Persona          string    // "employee" | "citizen"
	EmploymentStatus string    // "asn" | "non_asn" | "" (citizen)
	TenantID         string    // "" jika persona citizen atau belum pilih tenant
	CentralRoles     []string
	TenantRoles      []string
	TenantScope      []string // tenant tempat scoped central role berlaku
	IsCrossTenant    bool

	// Diisi codec saat Issue (jangan diisi pemanggil).
	TokenID   string    // jti — untuk revocation
	IssuedAt  time.Time // iat
	ExpiresAt time.Time // exp
}

// TokenIssuer menerbitkan token internal dari Claims. Dipakai alur login di identity
// (PR-2.4.3/2.4.4) lewat interface ini, bukan codec konkret.
type TokenIssuer interface {
	Issue(ctx context.Context, c Claims) (string, error)
}

// TokenVerifier memverifikasi token internal dan mengembalikan Claims-nya. Dipakai
// gateway (middleware auth, PR-2.4.2) lewat port ini tanpa import identity/ — seam yang
// sama dengan TenantResolver. Verify menolak token yang tanda tangannya tak sah, kedaluwarsa,
// salah issuer/audience, atau jti-nya sudah dicabut (revoked).
type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (*Claims, error)
}
