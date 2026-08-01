package crypto

import "context"

// Key realm (ADR-017). Sumbu partisi kunci di id.data_keys — dan field TenantID di
// port.FieldRef — memuat identitas REALM, bukan selalu identitas tenant. Ada dua jenis:
//
//	realm tenant  = <tenant_id>   → data tenant; hierarki ADR-010 §2, tak berubah
//	realm sentral = RealmCentral  → data identity (id.persons/employments/credentials)
//	                                + chain audit identity (id.audit_logs)
//
// Realm sentral ada karena data identity memang tak punya tenant: person hadir sebelum
// penugasan tenant mana pun (persona citizen tak pernah punya), dan satu person bisa
// melintas banyak tenant. Yang mengunci pilihan ini bukan estetika melainkan
// UNIQUE(nik_bidx) yang berlaku global se-identity-DB: kunci blind index per-tenant
// menghasilkan bidx berbeda untuk NIK yang sama, sehingga UNIQUE berhenti menangkap
// duplikat dan FindByNIK harus menyebut tenant yang ia tak punya (ADR-017 §Konteks).

// RealmCentral adalah identitas realm kunci sentral.
//
// Nilainya diawali garis bawah DENGAN SENGAJA: identity/domain.tenantIDRe mensyaratkan
// `^[a-z][a-z0-9-]{2,99}$`, jadi token ini mustahil menjadi tenant_id yang sah. Ketiadaan
// tabrakan bersifat struktural — tak ada CHECK constraint atau daftar nama terlarang yang
// harus dijaga tetap sinkron.
//
// Inilah sebabnya sentinel polos "central" ditolak (ADR-017 §Alternatif): ia nama tenant
// yang sah, sehingga pemda yang kebetulan didaftarkan dengan nama itu akan berbagi ruang
// kunci — dan chain audit — dengan realm sentral.
const RealmCentral = "_central"

// IsCentralRealm melaporkan apakah sebuah identitas realm menunjuk realm sentral.
func IsCentralRealm(realm string) bool { return realm == RealmCentral }

// centralRealmCustody menjawab custody realm sentral sebagai INVARIAN KODE, lalu meneruskan
// sisanya ke resolver di baliknya (ADR-017 §3).
//
// ADR-010 §3 menetapkan custody sebagai kebijakan per-tenant karena tiap pemda bisa punya
// kontrak berbeda soal siapa memegang kunci atas datanya sendiri. Realm sentral tak punya
// lawan bicara kontraktual: identity DB adalah DB platform yang memuat data seluruh pemda
// sekaligus, jadi tak ada satu pemda yang berwenang memegang KEK-nya.
//
// Menaruhnya di sini, bukan sebagai baris id.tenant_registry, juga menutup jalur perubahan
// diam-diam: tak ada baris yang bisa di-UPDATE menjadi custody 'tenant' (yang akan membuat
// identity DB tak terbaca dan baru ketahuan saat kripto dipakai).
//
// Bukan sekadar penghematan query: DBCustodyResolver fail-closed untuk tenant tak terdaftar,
// jadi tanpa dekorator ini realm sentral justru DITOLAK.
type centralRealmCustody struct {
	inner CustodyResolver
}

// WithCentralRealm membungkus resolver custody agar realm sentral selalu dijawab
// CustodyPlatform tanpa menyentuh registry. Aman dipanggil berlapis (idempoten secara
// perilaku) — pembungkusan ganda memberi jawaban yang sama.
func WithCentralRealm(inner CustodyResolver) CustodyResolver {
	if inner == nil {
		return nil
	}
	return centralRealmCustody{inner: inner}
}

func (c centralRealmCustody) Custody(ctx context.Context, realm string) (Custody, error) {
	if IsCentralRealm(realm) {
		return CustodyPlatform, nil
	}
	return c.inner.Custody(ctx, realm)
}
