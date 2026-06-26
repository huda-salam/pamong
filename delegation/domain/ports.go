package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DelegationRepository menyimpan & me-resolve delegasi di TENANT DB (gov.delegations).
// Didefinisikan di domain, diimplementasi di adapter/db. Domain tidak tahu Postgres.
type DelegationRepository interface {
	Save(ctx context.Context, d *Delegation) error
	// ListActiveByDelegatee mengembalikan delegasi yang AKTIF pada now untuk delegatee
	// (ToUserID) — kedaluwarsa difilter di sumber (lazy), sehingga delegasi lewat masa
	// berlaku tak pernah ikut ter-resolve (DoD PR-2.3.5b).
	ListActiveByDelegatee(ctx context.Context, toUserID uuid.UUID, now time.Time) ([]*Delegation, error)
}
