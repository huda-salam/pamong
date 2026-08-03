package sync

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
)

// RepoCloneSource mengimplementasi CloneSource di atas repo identity. Ia sengaja tipis:
// seluruh kerja kripto (membuka `{f}_enc` realm sentral, memeriksa purpose, pengikatan baris)
// sudah dilakukan repo — di sinilah letak alasan port ini ditempatkan di sisi identity dan
// bukan di sisi tenant. Penulis clone tak pernah menyentuh kunci realm sentral.
type RepoCloneSource struct {
	persons     domain.PersonRepository
	employments domain.EmploymentRepository
}

var _ CloneSource = (*RepoCloneSource)(nil)

// NewRepoCloneSource menolak repo nil: source yang setengah terpasang akan menghasilkan clone
// berpengenal kosong, kegagalan yang tak bergejala sampai seseorang mencoba me-resolve user.
func NewRepoCloneSource(p domain.PersonRepository, e domain.EmploymentRepository) (*RepoCloneSource, error) {
	if p == nil || e == nil {
		return nil, fmt.Errorf("identity/sync: CloneSource butuh PersonRepository & EmploymentRepository")
	}
	return &RepoCloneSource{persons: p, employments: e}, nil
}

// Identifiers membaca person (NIK/email/no_hp) dan employment (NIP) dari identity DB.
//
// Pesan error TIDAK memuat nilai pengenal apa pun — hanya id baris. Pesan FrameworkError
// mengalir ke log; ia jalur samping yang sama dengan yang sedang ditutup (ADR-009 §6).
func (s *RepoCloneSource) Identifiers(ctx context.Context, personID, employmentID uuid.UUID) (Identifiers, error) {
	p, err := s.persons.FindByID(ctx, personID)
	if err != nil {
		return Identifiers{}, fmt.Errorf("identity/sync: baca person %s untuk clone: %w", personID, err)
	}
	out := Identifiers{NIK: p.NIK, Email: p.Email, NoHP: p.NoHP}

	// Employment opsional: person tanpa kepegawaian tak punya NIP, dan non-ASN pun tidak.
	if employmentID == uuid.Nil {
		return out, nil
	}
	e, err := s.employments.FindByID(ctx, employmentID)
	if err != nil {
		return Identifiers{}, fmt.Errorf("identity/sync: baca employment %s untuk clone: %w", employmentID, err)
	}
	out.NIP = e.NIP
	return out, nil
}
