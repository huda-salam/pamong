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
// Isi clone TIDAK datang dari event — Engine memintanya ke CloneSource (lihat clone.go).
// Event hanya membawa koordinat (id) + atribut yang informasional.
//
// PENTING soal kegagalan handler: dengan driver NATS Core hari ini, error yang dikembalikan
// handler hanya DICATAT, tidak memicu pengiriman ulang (lihat infra/eventbus/nats.go —
// NATS Core tak punya re-delivery). Retry/DLQ yang ada berada di sisi TERBIT (OutboxRelay),
// bukan sisi konsumsi. Jadi gagal di sini berarti clone tidak tertulis sampai ada yang
// menanganinya: JetStream (durable consumer, ack eksplisit, MaxDeliver) atau job rekonsiliasi.
// Itu tetap pilihan yang benar dibanding menulis clone cacat secara senyap — tapi jangan
// mengandalkan "nanti juga di-retry" sebelum salah satu dari keduanya ada.
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
// Isi clone diambil dari identity DB di sini, bukan dari payload (PR-3.8.5b). Kegagalan baca
// menggagalkan handler tanpa menulis clone parsial — lihat catatan kegagalan di Engine.
//
// `NamaLengkap` sengaja diambil dari CloneAttributes meski payload juga membawanya: satu baris
// clone tak boleh mencampur nilai saat event terbit dengan nilai saat event ditangani. Yang di
// payload informasional (keterbacaan operator), bukan sumber kebenaran.
func (e *Engine) onEmploymentDitugaskan(ctx context.Context, ev port.Event) error {
	p, ok := ev.Payload.(domain.EmploymentDitugaskanPayload)
	if !ok {
		return fmt.Errorf("sync: payload %q bertipe %T, harap domain.EmploymentDitugaskanPayload", ev.Name, ev.Payload)
	}
	attr, err := e.source.Attributes(ctx, p.PersonID, p.EmploymentID)
	if err != nil {
		return err
	}
	return e.writer.Upsert(ctx, p.TenantID, UserProfileClone{
		PersonID:         p.PersonID,
		AssignmentID:     p.AssignmentID,
		NIK:              attr.NIK,
		NIP:              attr.NIP,
		NamaLengkap:      attr.NamaLengkap,
		EmploymentStatus: p.EmploymentStatus,
		IsCrossTenant:    p.IsCrossTenant,
		Email:            attr.Email,
		NoHP:             attr.NoHP,
	})
}
