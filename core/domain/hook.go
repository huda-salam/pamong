package domain

import (
	"errors"

	"github.com/huda-salam/pamong/port"
)

// Entity adalah representasi generik satu record yang dilewatkan ke lifecycle hook.
// Dipakai terutama oleh CRUD Tier 1 (tanpa struct domain khusus). Field disimpan sebagai
// map agar engine generik tidak bergantung pada tipe konkret modul.
type Entity struct {
	Name   string
	Fields map[string]any
}

// NewEntity membuat Entity kosong bernama name.
func NewEntity(name string) *Entity {
	return &Entity{Name: name, Fields: make(map[string]any)}
}

// Get mengembalikan nilai field (atau nil bila tidak ada).
func (e *Entity) Get(field string) any { return e.Fields[field] }

// Set menetapkan nilai field.
func (e *Entity) Set(field string, v any) {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[field] = v
}

// HookFunc adalah satu lifecycle hook. Menerima AuthContext (punya permission & tenant),
// bukan context biasa, sehingga hook bisa cek wewenang. Hook memanggil port/use case —
// TIDAK mengakses DB langsung (PRD anti-pattern).
type HookFunc func(ctx port.AuthContext, e *Entity) error

// HookSet mengelompokkan hook per fase. Hook dalam satu fase dieksekusi sesuai urutan
// list (deterministik).
type HookSet struct {
	BeforeSave   []HookFunc
	AfterSave    []HookFunc
	BeforeSubmit []HookFunc
	AfterSubmit  []HookFunc
	BeforeDelete []HookFunc
}

// Semantik (PRD F3):
//   - before_* return error  -> operasi dibatalkan (error diteruskan, transaksi rollback).
//   - after_*  return error  -> operasi tetap commit; error dikumpulkan untuk di-log/lapor,
//     TIDAK membatalkan yang sudah commit.

// RunBeforeSave menjalankan hook before-save berurutan; error pertama membatalkan.
func (h HookSet) RunBeforeSave(ctx port.AuthContext, e *Entity) error {
	return runUntilError(h.BeforeSave, ctx, e)
}

// RunBeforeSubmit menjalankan hook before-submit berurutan; error pertama membatalkan.
func (h HookSet) RunBeforeSubmit(ctx port.AuthContext, e *Entity) error {
	return runUntilError(h.BeforeSubmit, ctx, e)
}

// RunBeforeDelete menjalankan hook before-delete berurutan; error pertama membatalkan.
func (h HookSet) RunBeforeDelete(ctx port.AuthContext, e *Entity) error {
	return runUntilError(h.BeforeDelete, ctx, e)
}

// RunAfterSave menjalankan SEMUA hook after-save; error digabung (tidak membatalkan).
func (h HookSet) RunAfterSave(ctx port.AuthContext, e *Entity) error {
	return runAll(h.AfterSave, ctx, e)
}

// RunAfterSubmit menjalankan SEMUA hook after-submit; error digabung (tidak membatalkan).
func (h HookSet) RunAfterSubmit(ctx port.AuthContext, e *Entity) error {
	return runAll(h.AfterSubmit, ctx, e)
}

// runUntilError: berhenti & kembalikan error pertama (semantik before-hook).
func runUntilError(hooks []HookFunc, ctx port.AuthContext, e *Entity) error {
	for _, h := range hooks {
		if err := h(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// runAll: jalankan semua, kumpulkan error (semantik after-hook).
func runAll(hooks []HookFunc, ctx port.AuthContext, e *Entity) error {
	var errs []error
	for _, h := range hooks {
		if err := h(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
