package sync

import (
	"context"
	"fmt"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Engine mendaftarkan handler clone ke event bus dan menuliskan hasilnya lewat Writer.
// Ia adalah driven consumer: event masuk → clone keluar ke tenant DB.
type Engine struct {
	writer Writer
}

func NewEngine(writer Writer) *Engine { return &Engine{writer: writer} }

// Register men-subscribe handler ke event identity yang memengaruhi clone tenant.
// Saat ini: employment.ditugaskan → buat/segarkan clone gov.user_profiles di tenant
// tujuan (DoD PR-2.2.4). Event lain (person.diperbarui, employment.dicabut) menyusul.
func (e *Engine) Register(sub port.EventSubscriber) error {
	return sub.Subscribe(domain.EventEmploymentDitugaskan, e.onEmploymentDitugaskan)
}

// onEmploymentDitugaskan meng-clone person ke gov.user_profiles tenant tujuan. Payload
// di-assert ke tipe terdaftar; ketidakcocokan tipe = bug schema, dikembalikan sebagai error.
func (e *Engine) onEmploymentDitugaskan(ctx context.Context, ev port.Event) error {
	p, ok := ev.Payload.(domain.EmploymentDitugaskanPayload)
	if !ok {
		return fmt.Errorf("sync: payload %q bertipe %T, harap domain.EmploymentDitugaskanPayload", ev.Name, ev.Payload)
	}
	return e.writer.Upsert(ctx, p.TenantID, UserProfileClone{
		PersonID:         p.PersonID,
		AssignmentID:     p.AssignmentID,
		NIK:              p.NIK,
		NIP:              p.NIP,
		NamaLengkap:      p.NamaLengkap,
		EmploymentStatus: p.EmploymentStatus,
		IsCrossTenant:    p.IsCrossTenant,
	})
}
