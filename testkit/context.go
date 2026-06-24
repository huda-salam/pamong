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
}

var _ port.AuthContext = (*TestContext)(nil)

type Option func(*TestContext)

// WithTenant menyetel tenant_id konteks.
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

// context.Context forwarding
func (c *TestContext) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c *TestContext) Done() <-chan struct{}       { return c.Context.Done() }
func (c *TestContext) Err() error                  { return c.Context.Err() }
func (c *TestContext) Value(key any) any           { return c.Context.Value(key) }
