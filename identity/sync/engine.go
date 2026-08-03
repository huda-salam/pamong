package sync

import (
	"context"
	"fmt"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Engine mendaftarkan handler clone ke event bus dan menuliskan hasilnya lewat Writer.
// Ia adalah driven consumer: event masuk → clone keluar ke tenant DB.
//
// Pengenal (nik/nip/email/no_hp) TIDAK datang dari event — Engine memintanya ke CloneSource
// (lihat clone.go). Event hanya membawa koordinat (id) + atribut non-pengenal.
type Engine struct {
	writer Writer
	source CloneSource
}

// NewEngine menolak source nil. Tanpa CloneSource, satu-satunya cara Engine bisa melanjutkan
// adalah menulis clone berpengenal kosong — baris yang lolos tanpa error tapi tak bisa
// di-resolve dan tak bisa dinotifikasi.
func NewEngine(writer Writer, source CloneSource) (*Engine, error) {
	if writer == nil || source == nil {
		return nil, fmt.Errorf("identity/sync: Engine butuh Writer & CloneSource")
	}
	return &Engine{writer: writer, source: source}, nil
}

// Register men-subscribe handler ke event identity yang memengaruhi clone tenant.
// Saat ini: employment.ditugaskan → buat/segarkan clone gov.user_profiles di tenant
// tujuan (DoD PR-2.2.4). Event lain (person.diperbarui, employment.dicabut) menyusul.
func (e *Engine) Register(sub port.EventSubscriber) error {
	return sub.Subscribe(domain.EventEmploymentDitugaskan, e.onEmploymentDitugaskan)
}

// onEmploymentDitugaskan meng-clone person ke gov.user_profiles tenant tujuan. Payload
// di-assert ke tipe terdaftar; ketidakcocokan tipe = bug schema, dikembalikan sebagai error.
//
// Pengenal diambil dari identity DB di sini, bukan dari payload (PR-3.8.5b). Kegagalan baca
// menggagalkan handler → event di-retry; tak ada clone parsial yang ditulis.
func (e *Engine) onEmploymentDitugaskan(ctx context.Context, ev port.Event) error {
	p, ok := ev.Payload.(domain.EmploymentDitugaskanPayload)
	if !ok {
		return fmt.Errorf("sync: payload %q bertipe %T, harap domain.EmploymentDitugaskanPayload", ev.Name, ev.Payload)
	}
	ids, err := e.source.Identifiers(ctx, p.PersonID, p.EmploymentID)
	if err != nil {
		return err
	}
	return e.writer.Upsert(ctx, p.TenantID, UserProfileClone{
		PersonID:         p.PersonID,
		AssignmentID:     p.AssignmentID,
		NIK:              ids.NIK,
		NIP:              ids.NIP,
		NamaLengkap:      p.NamaLengkap,
		EmploymentStatus: p.EmploymentStatus,
		IsCrossTenant:    p.IsCrossTenant,
		Email:            ids.Email,
		NoHP:             ids.NoHP,
	})
}
