// Package testkit menyediakan mock, fixture, dan helper untuk unit test modul bisnis.
// Tidak boleh diimport dari kode produksi — hanya untuk package _test.
package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// TestContext mengimplementasi port.AuthContext dengan kontrol penuh permission.
// Dibuat via NewContext(t, ...Option).
type TestContext struct {
	context.Context
	t        *testing.T
	tenantID string
	personID uuid.UUID
	perms    map[string]bool
	roles    map[string]bool
	// units = jangkauan unit kerja actor bila test menyatakannya (WithUnitAuthority). nil =
	// unit diabaikan; non-nil = hanya unit di dalamnya yang lolos RequirePermissionInUnit.
	units map[uuid.UUID]bool
	// subtrees = unit yang jangkauannya MENYERTAKAN keturunan (WithSubtreeAuthority). Dipisah
	// dari units karena itu justru pertanyaan yang berbeda: memberi `include_subtree` menuntut
	// wewenang atas turunan, bukan atas unit itu saja (ADR-021).
	subtrees map[uuid.UUID]bool
}

var _ port.AuthContext = (*TestContext)(nil)

type Option func(*TestContext)

// WithTenant menyetel tenant_id konteks. Nilainya juga disuntikkan sebagai value context
// lewat port.WithTenant (lihat NewContext) — persis seperti gateway.Context.SetTenantID di
// runtime, sehingga driven adapter yang membaca port.TenantFrom (routing DB per-tenant,
// enkripsi field per-tenant) berperilaku sama di test dan di produksi.
func WithTenant(id string) Option {
	return func(c *TestContext) { c.tenantID = id }
}

// WithPermission menambahkan satu permission ke konteks.
func WithPermission(perm string) Option {
	return func(c *TestContext) { c.perms[perm] = true }
}

// WithRole menambahkan satu role ke konteks.
func WithRole(role string) Option {
	return func(c *TestContext) { c.roles[role] = true }
}

// WithUnitAuthority menyatakan jangkauan unit actor secara EKSPLISIT: hanya unit yang disebut
// (dan hanya itu) yang lolos RequirePermissionInUnit. uuid.Nil berarti wewenang SE-TENANT —
// jangkauan TERLUAS, sesuai konvensi core/permission.RequireAuthorityOver — sehingga ia meloloskan
// setiap unit konkret DAN setiap subtree, persis seperti grant TenantWide di produksi (lihat
// tenantWide).
//
// Sekali dipakai, konteks berhenti mengabaikan unit; unit yang tak disebut ditolak. Itulah yang
// membuat test containment bisa gagal saat aturannya dilepas.
func WithUnitAuthority(units ...uuid.UUID) Option {
	return func(c *TestContext) {
		if c.units == nil {
			c.units = make(map[uuid.UUID]bool)
		}
		for _, u := range units {
			c.units[u] = true
		}
	}
}

// WithSubtreeAuthority menyatakan actor berwenang atas unit BESERTA KETURUNANNYA. Ia juga
// memberi wewenang atas unit itu sendiri (superset dari WithUnitAuthority).
func WithSubtreeAuthority(units ...uuid.UUID) Option {
	return func(c *TestContext) {
		if c.subtrees == nil {
			c.subtrees = make(map[uuid.UUID]bool)
		}
		if c.units == nil {
			c.units = make(map[uuid.UUID]bool)
		}
		for _, u := range units {
			c.subtrees[u] = true
			c.units[u] = true
		}
	}
}

// WithPersonID menyetel person_id konteks.
func WithPersonID(id uuid.UUID) Option {
	return func(c *TestContext) { c.personID = id }
}

// Ctx adalah alias ringkas NewContext, sesuai pemakaian di contoh CLAUDE.md
// (testkit.Ctx(t, testkit.WithRole("..."))).
func Ctx(t *testing.T, opts ...Option) *TestContext { return NewContext(t, opts...) }

// NewContext membuat TestContext dengan opsi yang diberikan.
func NewContext(t *testing.T, opts ...Option) *TestContext {
	t.Helper()
	c := &TestContext{
		Context:  context.Background(),
		t:        t,
		personID: uuid.New(),
		perms:    make(map[string]bool),
		roles:    make(map[string]bool),
	}
	for _, o := range opts {
		o(c)
	}
	// Tenant disuntikkan ke value context, bukan hanya ke field TenantID(): itulah satu-satunya
	// sumber yang dibaca port.TenantFrom sejak fallback assertion AuthContext dihapus. Tanpa
	// ini, test yang memakai TestContext akan melihat tenant kosong di lapis repository
	// (enkripsi field gagal / audit ter-redaksi) padahal runtime menyediakannya.
	if c.tenantID != "" {
		c.Context = port.WithTenant(c.Context, c.tenantID)
	}
	return c
}

func (c *TestContext) PersonID() uuid.UUID             { return c.personID }
func (c *TestContext) Persona() string                 { return "employee" }
func (c *TestContext) EmploymentStatus() string        { return "asn" }
func (c *TestContext) TenantID() string                { return c.tenantID }
func (c *TestContext) IsCitizen() bool                 { return false }
func (c *TestContext) IsCrossTenant() bool             { return false }
func (c *TestContext) HasRole(role string) bool        { return c.roles[role] }
func (c *TestContext) HasCentralRole(role string) bool { return false }

// RequirePermission mengembalikan ErrPermissionDenied jika permission tidak ada di konteks.
func (c *TestContext) RequirePermission(perm string) error {
	if !c.perms[perm] {
		return core.ErrPermissionDenied(perm)
	}
	return nil
}

// RequirePermissionInUnit memeriksa kepemilikan permission, lalu — HANYA bila test menyatakan
// jangkauan lewat WithUnitAuthority — memeriksa apakah unit sasaran ada dalam jangkauan itu.
//
// Default tanpa opsi tetap mengabaikan unit: mayoritas unit test modul tak berurusan dengan ABAC,
// dan memaksa mereka menyatakan jangkauan hanya menambah derau. Tapi default itu TIDAK CUKUP untuk
// use case yang MEMBERIKAN wewenang ber-scope (penugasan role, delegasi): di sana "unit diabaikan"
// berarti test hijau untuk containment yang tak ada (ADR-021). Karena itu opsi ini disediakan di
// testkit, bukan diakali per-paket dengan konteks tiruan sendiri.
func (c *TestContext) RequirePermissionInUnit(perm string, unitID uuid.UUID) error {
	if err := c.RequirePermission(perm); err != nil {
		return err
	}
	if c.units == nil {
		return nil // test tak menyatakan jangkauan → unit diabaikan (perilaku lama)
	}
	if c.tenantWide() || c.units[unitID] {
		return nil
	}
	return core.ErrPermissionDenied(perm)
}

// tenantWide melaporkan apakah test menyatakan wewenang SE-TENANT (WithUnitAuthority(uuid.Nil)).
//
// Ia bukan kenyamanan, melainkan penyelarasan dengan produksi: uuid.Nil di sana bukan "sebuah unit
// bernama nol" melainkan pertanyaan "punya grant TenantWide?" — dan grant TenantWide menutupi
// SETIAP unit beserta keturunannya (permission.Grant.TenantWide, dicek lebih dulu di covers &
// coversSubtree). Bila di sini uuid.Nil diperlakukan sebagai kunci map biasa, fake ini MENOLAK
// pemeriksaan unit konkret yang produksi izinkan; test yang lulus dengannya menegakkan invariant
// yang tak ada di produksi — kegagalan yang arahnya paling berbahaya untuk sebuah fake otorisasi:
// ia terlihat lebih ketat, jadi tak ada yang curiga.
func (c *TestContext) tenantWide() bool { return c.units[uuid.Nil] }

// RequirePermissionInSubtree memeriksa kepemilikan permission lalu — bila test menyatakan
// jangkauan — menuntut jangkauan yang MENYERTAKAN keturunan (WithSubtreeAuthority).
//
// Perhatikan asimetrinya, dan itu memang inti aturannya: WithUnitAuthority saja TIDAK cukup di
// sini. Test yang mengharapkan `include_subtree` lolos dengan wewenang satu unit sedang menyatakan
// eskalasi sebagai perilaku yang benar.
func (c *TestContext) RequirePermissionInSubtree(perm string, unitID uuid.UUID) error {
	if err := c.RequirePermission(perm); err != nil {
		return err
	}
	if c.units == nil {
		return nil // test tak menyatakan jangkauan → unit diabaikan (perilaku lama)
	}
	if c.tenantWide() || c.subtrees[unitID] {
		return nil
	}
	return core.ErrPermissionDenied(perm)
}

// context.Context forwarding
func (c *TestContext) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c *TestContext) Done() <-chan struct{}       { return c.Context.Done() }
func (c *TestContext) Err() error                  { return c.Context.Err() }
func (c *TestContext) Value(key any) any           { return c.Context.Value(key) }
