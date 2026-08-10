// admin_identity.go merakit grup HTTP administrasi identity (`/admin/identity/*`, PR-W2) di
// composition root, dan memasangnya pada router bisnis.
//
// Ia menutup GAP (b) PR-5.1.4. Clone engine identity→tenant sudah ter-wire sejak PR itu, tapi
// tak ada satu pun kode produksi yang menerbitkan `identity.employment.ditugaskan` — jadi jalur
// clone tak pernah berjalan di luar test, dan `gov.user_profiles` di server hidup selalu kosong.
// `AssignEmploymentToTenant` adalah produsen event itu; memasang permukaan HTTP-nya adalah yang
// menghidupkan jalurnya.
//
// Semua di sini murni WIRING: tak ada aturan bisnis yang ditulis ulang. Kebijakan permission ada
// di use case, kebijakan kripto di crypto.FieldSealer, kebijakan audit di dekorator repo.
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/huda-salam/pamong/core/audit"
	identityauth "github.com/huda-salam/pamong/identity/adapter/auth"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identityhttp "github.com/huda-salam/pamong/identity/adapter/http"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// adminIdentityRoutes adalah kontrak minimal yang dibutuhkan mountAdminIdentityRoutes, dipenuhi
// oleh *identity/adapter/http.AdminHandler. Seam ini ada agar PEMASANGAN rute (dan karenanya
// stack middleware di sekelilingnya — terutama RequireAuth) dapat diuji tanpa merakit identity
// beserta DB-nya. Cermin authRoutes untuk grup /auth/*, dengan kesimpulan yang berlawanan: grup
// ini justru HARUS berada di balik RequireAuth.
type adminIdentityRoutes interface {
	CreatePerson(http.ResponseWriter, *http.Request)
	AttachEmployment(http.ResponseWriter, *http.Request)
	CreateCredential(http.ResponseWriter, *http.Request)
	AssignEmploymentToTenant(http.ResponseWriter, *http.Request)
	AssignCentralRole(http.ResponseWriter, *http.Request)
}

// wireAdminIdentity merakit use case administrasi identity beserta repo ber-AUDIT.
//
// Pilihan perakitan yang tidak sembarang:
//
//  1. **Repo dibungkus dekorator audit** — kebalikan dari `wireAuth` dan `wireIdentitySync`.
//     Keduanya sengaja memakai repo telanjang karena alur login & clone hanya MEMBACA (dan
//     login belum punya aktor). Grup ini seluruhnya MUTASI identitas oleh aktor yang sudah
//     terotentikasi, jadi ADR-003 berlaku penuh: setiap baris yang lahir dari sini punya jejak
//     siapa-kapan-apa. `recordAudit` menuntut ctx berupa port.AuthContext — terpenuhi karena
//     handler meneruskan gateway.Context.
//
//  2. **`AuditStore.EnsureSchema` dipanggil saat boot, bukan lazy.** `id.audit_logs` lahir dari
//     DDL di Go (jalur C), dan penulisan pertamanya terjadi di tengah mutasi identity. Gagal
//     membuat tabel di sana berarti mutasi sudah commit sementara jejaknya hilang; gagal di
//     sini berarti server tak boot sama sekali (fail-fast, philosophy #4).
//
//  3. **`cryptoSvc` yang sama dengan sisa proses.** Diff audit memuat NIK/NIP/cred_value, jadi
//     dekoratornya menyegel nilai itu dengan realm SENTRAL (ADR-017) sebelum mencatat —
//     mengenkripsi kolom tanpa ini hanya MEMINDAHKAN kebocoran ke id.audit_logs (REVIEW_BACKLOG
//     E2). Konstruktornya menolak CryptoPort nil, jadi salah rakit gagal saat boot.
//
//  4. **`verifyGate` DITERIMA dari run(), bukan dibuat di sini.** `CreateCredential` menghitung
//     hash bcrypt, dan bcrypt terikat CPU: gerbang terpisah untuk permukaan ini akan
//     melipatgandakan batas concurrency yang justru ingin ditegakkan seluruh proses. Rute ini
//     memang ber-token & ber-permission, tapi rate limit gateway per-principal ada di orde
//     ratusan rps — satu admin sah yang membanjirinya cukup menjenuhkan seluruh core dan
//     menjatuhkan jalur login bersamanya. Instance yang sama diteruskan ke wireAuth.
//
//  5. **Publisher = bus yang sama.** Event `identity.employment.ditugaskan` yang terbit dari
//     sini adalah yang dikonsumsi clone engine (wireIdentitySync). Bus berbeda = penugasan
//     berhasil, clone tak pernah terjadi, dan tak ada satu pun error yang muncul.
func wireAdminIdentity(
	ctx context.Context,
	identityPool *db.Pool,
	cryptoSvc port.CryptoPort,
	pub port.EventPublisher,
	verifyGate *usecase.VerifyGate,
) (*identityhttp.AdminHandler, error) {
	auditStore := identitydb.NewAuditStore(identityPool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		return nil, fmt.Errorf("skema audit identity (id.audit_logs): %w", err)
	}
	auditEngine := audit.NewEngine(auditStore)

	personRepo, err := identitydb.NewPersonRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("repo person: %w", err)
	}
	persons, err := identitydb.NewAuditedPersonRepo(personRepo, auditEngine, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("dekorator audit person: %w", err)
	}

	employmentRepo, err := identitydb.NewEmploymentRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("repo employment: %w", err)
	}
	employments, err := identitydb.NewAuditedEmploymentRepo(employmentRepo, auditEngine, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("dekorator audit employment: %w", err)
	}

	credentialRepo, err := identitydb.NewCredentialRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("repo credential: %w", err)
	}
	credentials, err := identitydb.NewAuditedCredentialRepo(credentialRepo, auditEngine, cryptoSvc)
	if err != nil {
		return nil, fmt.Errorf("dekorator audit credential: %w", err)
	}

	assignments := identitydb.NewAuditedTenantAssignmentRepo(
		identitydb.NewTenantAssignmentRepo(identityPool), auditEngine)
	roles := identitydb.NewAuditedCentralRoleRepo(
		identitydb.NewCentralRoleRepo(identityPool), auditEngine)
	roleAssignments := identitydb.NewAuditedCentralRoleAssignmentRepo(
		identitydb.NewCentralRoleAssignmentRepo(identityPool), auditEngine)
	tenants := identitydb.NewTenantRepo(identityPool)

	return identityhttp.NewAdminHandler(
		usecase.NewCreatePerson(persons, pub),
		usecase.NewAttachEmployment(persons, employments, pub),
		usecase.NewCreateCredential(persons, credentials, identityauth.NewBcryptVerifier(), verifyGate),
		usecase.NewAssignEmploymentToTenant(persons, employments, assignments, tenants, pub),
		usecase.NewAssignCentralRole(roles, roleAssignments),
	), nil
}

// mountAdminIdentityRoutes memasang grup /admin/identity/* pada ROUTER BISNIS — bukan pada top
// mux seperti /auth/*.
//
// Bedanya menentukan dan disengaja. Router bisnis dibungkus stack lengkap
// (Auth → RequireAuth → TenantResolver → RateLimit → Idempotency), dan setiap lapisnya memang
// yang dibutuhkan grup ini: rutenya menuntut token (RequireAuth), aktornya harus ber-principal
// nyata agar rate limit ber-kunci per-orang punya arti, dan seluruhnya mutasi sehingga
// Idempotency-Key layak dihormati. Memasangnya di top mux akan menuntut menyalin ulang stack itu
// — dan salinan yang tertinggal satu lapis tak akan bergejala sampai ada yang menyerangnya.
//
// TenantResolver ikut terlewati tanpa masalah bagi admin platform yang tokennya tak ber-tenant:
// klaim tenant kosong diteruskan apa adanya, dan use case di sini memang bekerja pada identity
// DB sentral, bukan pada DB tenant mana pun.
func mountAdminIdentityRoutes(r port.Router, h adminIdentityRoutes) {
	r.Post("/admin/identity/persons", h.CreatePerson)
	r.Post("/admin/identity/employments", h.AttachEmployment)
	r.Post("/admin/identity/credentials", h.CreateCredential)
	r.Post("/admin/identity/assignments", h.AssignEmploymentToTenant)
	r.Post("/admin/identity/central-role-assignments", h.AssignCentralRole)
}
