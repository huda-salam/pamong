package workflow_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/workflow"
)

// `notify:` tak lengkap adalah notifikasi yang MUSTAHIL berhasil, bukan yang dimatikan —
// engine tetap memanggilnya, lalu gagal render/resolusi tanpa satu pun request yang error.
func TestValidate_NotifyTanpaTemplateDitolak(t *testing.T) {
	def := defDenganNotify(&workflow.NotifySpec{ToRole: "agendaris"})
	if err := workflow.Validate(def); err == nil {
		t.Fatal("notify tanpa template diterima")
	}
}

func TestValidate_NotifyTanpaToRoleDitolak(t *testing.T) {
	def := defDenganNotify(&workflow.NotifySpec{Template: "modul.x"})
	if err := workflow.Validate(def); err == nil {
		t.Fatal("notify tanpa to_role diterima")
	}
}

func TestValidate_NotifyLengkapDiterima(t *testing.T) {
	def := defDenganNotify(&workflow.NotifySpec{ToRole: "agendaris", Template: "modul.x"})
	if err := workflow.Validate(def); err != nil {
		t.Fatalf("notify lengkap ditolak: %v", err)
	}
}

// Tanpa blok notify sama sekali = notifikasi memang dimatikan. Itu jalan yang sah.
func TestValidate_TanpaNotifyDiterima(t *testing.T) {
	if err := workflow.Validate(defDenganNotify(nil)); err != nil {
		t.Fatalf("transisi tanpa notify ditolak: %v", err)
	}
}

func defDenganNotify(n *workflow.NotifySpec) workflow.WorkflowDefinition {
	return workflow.WorkflowDefinition{
		ID: "uji.notify.standar", Entity: "uji.X", Version: 1, InitialState: "mulai",
		States: []workflow.State{
			{Name: "mulai"},
			{Name: "selesai", IsTerminal: true},
		},
		Transitions: []workflow.Transition{
			{From: "mulai", To: "selesai", On: "selesaikan", Notify: n},
		},
	}
}
