package domain_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/identity/domain"
)

// Sentinel punya DUA sumber kebenaran yang tak bisa saling membaca: konstanta Go dan literal di
// `identity/migrations/010_seed_system_actor.up.sql` (SQL tak bisa mengimpor paket Go). Bila
// keduanya menyimpang, tak ada satu pun yang mengeluh — `assigned_by` sekadar menunjuk baris yang
// tak ada dan setiap penugasan bootstrap gagal dengan pelanggaran FK yang membingungkan, atau
// lebih buruk: menunjuk baris LAIN yang kebetulan ada. Test ini yang mengikat keduanya.
func TestSystemActor_KonstantaSelarasDenganMigrasi(t *testing.T) {
	sql := bacaMigrasiSentinel(t)

	if !strings.Contains(sql, domain.SystemActorID.String()) {
		t.Fatalf("migrasi 010 tak memuat SystemActorID %q — konstanta Go & seed SQL menyimpang",
			domain.SystemActorID)
	}
	if !strings.Contains(sql, domain.SystemActorNama) {
		t.Fatalf("migrasi 010 tak memuat SystemActorNama %q", domain.SystemActorNama)
	}
	// Baris sentinel WAJIB masuk ke id.persons: FK `assigned_by` menunjuk tabel itu, jadi seed ke
	// tabel lain memenuhi test di atas tanpa menyelesaikan apa pun.
	if !strings.Contains(sql, "id.persons") {
		t.Fatal("migrasi 010 tak menyisipkan ke id.persons — FK assigned_by tak akan terpenuhi")
	}
}

// Nilainya harus JELAS bukan UUID acak, dan tak boleh bisa lahir dari uuid.New(): versi & varian
// nol membuatnya bukan UUIDv4 yang sah, sehingga tabrakan dengan id yang di-generate mustahil.
func TestSystemActor_BukanUUIDAcak(t *testing.T) {
	const mau = "00000000-0000-0000-0000-000000000001"
	if got := domain.SystemActorID.String(); got != mau {
		t.Fatalf("SystemActorID = %q, mau %q — nilainya kontrak dengan migrasi & data produksi "+
			"yang sudah menunjuknya; mengubahnya membuat baris lama menggantung", got, mau)
	}
	if v := domain.SystemActorID.Version(); v != 0 {
		t.Fatalf("versi UUID sentinel = %d, mau 0 (bukan UUID yang bisa lahir dari uuid.New())", v)
	}
}

// Sentinel tak bisa dibuat ulang, ditimpa, atau ditiru lewat jalur tulis biasa: Person.Validate
// menolak NIK kosong, jadi satu-satunya penulisnya adalah migrasi. Sifat ini yang membuat baris
// ber-nik_bidx kosong aman menempati index UNIQUE.
func TestSystemActor_TakBisaDibuatLewatRepo(t *testing.T) {
	p := &domain.Person{
		ID: domain.SystemActorID, NIK: "", NamaLengkap: domain.SystemActorNama, IsActive: false,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Person ber-NIK kosong lolos Validate — sentinel jadi bisa dibuat/ditimpa lewat " +
			"PersonRepo.Save, dan baris ber-bidx kosong berhenti unik")
	}
}

// bacaMigrasiSentinel membaca file migrasi 010 relatif terhadap file test ini.
func bacaMigrasiSentinel(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(thisFile), "..", "migrations", "010_seed_system_actor.up.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baca migrasi sentinel: %v", err)
	}
	return string(b)
}
