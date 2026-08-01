package db

import (
	"testing"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/crypto"
)

// TestRealmSentralTakBisaJadiTenantID adalah properti yang menopang seluruh ADR-017:
// ketidakmungkinan tabrakan antara realm sentral dan tenant nyata bersifat STRUKTURAL
// (validator tenant menolak token itu), bukan bergantung pada tak adanya pemda yang
// kebetulan memakai nama tersebut.
//
// Test ini duduk di adapter, bukan di identity/domain, karena domain tak boleh mengimport
// infra (aturan hexagonal + linter domain-no-infra-import). Yang dibandingkan tetap
// validator domain NYATA, bukan salinan regex-nya.
func TestRealmSentralTakBisaJadiTenantID(t *testing.T) {
	tenant := &domain.Tenant{
		TenantID: crypto.RealmCentral,
		Nama:     "Percobaan mendaftarkan realm sentral sebagai tenant",
		Tier:     domain.TierShared,
		DBHost:   "db1",
		DBName:   "gov_palsu",
	}
	if err := tenant.Validate(); err == nil {
		t.Fatalf("tenant_id %q harus DITOLAK — bila ia sah, satu pemda bisa berbagi "+
			"ruang kunci & chain audit dengan realm sentral", crypto.RealmCentral)
	}

	// Kebalikannya juga harus benar: sentinel yang DITOLAK ADR-017 justru diterima
	// validator, dan itulah alasan ia tak dipakai.
	polos := *tenant
	polos.TenantID = "central"
	if err := polos.Validate(); err != nil {
		t.Fatalf("premis ADR-017 gugur: %q ternyata bukan tenant_id yang sah (%v) — "+
			"bila validator berubah, tinjau ulang pilihan token realm", polos.TenantID, err)
	}
}

// TestPurposeOfCredType mengunci pemetaan purpose kredensial (ADR-017 §4). Ia tampak sepele,
// tapi purpose menentukan KUNCI dan NORMALISASI: menggabungkannya kembali menjadi satu nilai
// akan mematikan case-folding email tanpa satu pun error.
func TestPurposeOfCredType(t *testing.T) {
	for credType, want := range map[domain.CredType]string{
		domain.CredNIK:   "nik",
		domain.CredNIP:   "nip",
		domain.CredEmail: "email",
		domain.CredNoHP:  "no_hp",
		domain.CredOAuth: "oauth",
	} {
		if got := purposeOfCredType(credType); got != want {
			t.Errorf("purposeOfCredType(%q) = %q, mau %q", credType, got, want)
		}
	}
}

// TestPurposeKolomPersonSelarasDenganDiffAudit menjaga janji ADR-017 §4: kolom dan diff
// audit-nya memakai purpose yang SAMA. Bila keduanya menyimpang, nilai yang disegel di satu
// tempat tak bisa dibuka dengan kunci tempat lain — dan tak ada yang tahu sampai seorang
// auditor mencoba membacanya.
func TestPurposeKolomPersonSelarasDenganDiffAudit(t *testing.T) {
	kolom := map[string]string{
		"nik":   purposeNIK,
		"no_hp": purposeNoHP,
		"email": purposeEmail,
	}
	for _, s := range personSensitiveFields {
		want, ok := kolom[s.Column]
		if !ok {
			t.Errorf("kolom diff %q tak punya pasangan purpose kolom person", s.Column)
			continue
		}
		if s.Purpose != want {
			t.Errorf("purpose diff %q = %q, tapi kolomnya memakai %q", s.Column, s.Purpose, want)
		}
	}
	if len(employmentSensitiveFields) != 1 || employmentSensitiveFields[0].Purpose != purposeNIP {
		t.Errorf("diff employment harus menyegel NIP dengan purpose %q, dapat %+v",
			purposeNIP, employmentSensitiveFields)
	}
}
