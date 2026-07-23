// Package strategy menyediakan "selectable strategy": satu titik keputusan (decision
// point) dengan beberapa varian algoritma/kebijakan sah yang dipilih per-tenant lewat
// KEY, bukan lewat percabangan if-else atau logika tersimpan di DB (CLAUDE.md §Mekanisme 1,
// core/strategy/PRD.md). Logika tetap kode Go ter-test; yang configurable hanya identifier.
//
// PR-3.3.1: registry ber-key + resolusi pilihan (F1, F2). Filter opsi rule-tier (F3),
// versioning pilihan (F4), dan validator koherensi (F5) menyusul di PR-3.3.2..3.3.5.
package strategy

import (
	"fmt"

	"github.com/huda-salam/pamong/core"
)

// ErrInvalidKey dipublikasikan saat format strategy key tidak sesuai konvensi
// {modul}.{titik}.{varian} (minimal 3 segmen non-kosong) (HTTP 422).
func ErrInvalidKey(key, reason string) error {
	return core.ErrValidation("strategy_key", fmt.Sprintf("key %q: %s", key, reason))
}

// ErrKeyAlreadyRegistered dipublikasikan saat sebuah strategy key didaftarkan dua kali —
// registrasi ganda menandakan bug wiring modul (HTTP 409).
func ErrKeyAlreadyRegistered(key string) error {
	return core.ErrConflict(fmt.Sprintf("strategy key %q sudah terdaftar", key))
}

// ErrKeyNotRegistered dipublikasikan saat pilihan tenant merujuk key yang tidak terdaftar
// untuk decision point tsb — tidak ada fallback diam-diam (PRD F2) (HTTP 422).
func ErrKeyNotRegistered(key, decisionPoint string) error {
	return core.ErrValidation("strategy_key",
		fmt.Sprintf("key %q tidak terdaftar untuk decision point %q", key, decisionPoint))
}

// ErrNoSelection dipublikasikan saat sebuah decision point tidak punya pilihan tenant
// maupun default developer — caller wajib menetapkan pilihan (PRD F2) (HTTP 404).
func ErrNoSelection(tenantID, decisionPoint string) error {
	return core.ErrNotFound("StrategySelection",
		fmt.Sprintf("tenant=%s decision_point=%s", tenantID, decisionPoint))
}

// ErrUnknownDecisionPoint dipublikasikan saat decision point tidak punya varian terdaftar
// sama sekali (HTTP 404).
func ErrUnknownDecisionPoint(decisionPoint string) error {
	return core.ErrNotFound("StrategyDecisionPoint", decisionPoint)
}
