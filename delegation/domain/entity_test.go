package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/delegation/domain"
)

func validDelegation() *domain.Delegation {
	return &domain.Delegation{
		ID:          uuid.New(),
		FromUserID:  uuid.New(),
		ToUserID:    uuid.New(),
		Permissions: []string{"surat_masuk:surat:baca"},
		ValidFrom:   time.Now(),
		ValidUntil:  time.Now().Add(24 * time.Hour),
		AssignedBy:  uuid.New(),
	}
}

func TestDelegation_Validate_OK(t *testing.T) {
	if err := validDelegation().Validate(); err != nil {
		t.Fatalf("delegasi valid ditolak: %v", err)
	}
}

func TestDelegation_Validate_Errors(t *testing.T) {
	cases := map[string]func(*domain.Delegation){
		"ke diri sendiri":    func(d *domain.Delegation) { d.ToUserID = d.FromUserID },
		"permissions kosong": func(d *domain.Delegation) { d.Permissions = nil },
		"valid_until wajib":  func(d *domain.Delegation) { d.ValidUntil = time.Time{} },
		"periode terbalik": func(d *domain.Delegation) {
			d.ValidFrom = time.Now()
			d.ValidUntil = d.ValidFrom.Add(-time.Hour)
		},
		"from kosong": func(d *domain.Delegation) { d.FromUserID = uuid.Nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := validDelegation()
			mutate(d)
			if err := d.Validate(); err == nil {
				t.Errorf("%s seharusnya ditolak", name)
			}
		})
	}
}

func TestDelegation_AppliesTo(t *testing.T) {
	now := time.Now()
	d := &domain.Delegation{ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	if !d.AppliesTo(now) {
		t.Error("delegasi dalam masa berlaku harus aktif")
	}
	// Kedaluwarsa: valid_until lewat → tidak aktif (DoD PR-2.3.5b).
	expired := &domain.Delegation{ValidFrom: now.Add(-2 * time.Hour), ValidUntil: now.Add(-time.Hour)}
	if expired.AppliesTo(now) {
		t.Error("delegasi kedaluwarsa tidak boleh aktif")
	}
	// Belum mulai.
	future := &domain.Delegation{ValidFrom: now.Add(time.Hour), ValidUntil: now.Add(2 * time.Hour)}
	if future.AppliesTo(now) {
		t.Error("delegasi yang belum mulai tidak boleh aktif")
	}
}
