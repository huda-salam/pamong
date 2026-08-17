package schema_test

import (
	"strings"
	"testing"

	coreSched "github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/infra/schema"
)

// TestResidensi_SchedulerHanyaDiJalurSentral mengunci ADR-023: tabel scheduler hidup di DB
// SENTRAL, jadi migrasinya tak boleh ikut jalur tenant.
//
// Kenapa test ini ada padahal isinya sepele: satu-satunya yang memisahkan tenant dari sentral
// adalah keanggotaan daftar di sources.go — nama schema kedua jalur sama-sama `gov`. Salah
// daftar menempatkan tabel di DB yang keliru TANPA satu pun error, dan gejalanya (jadwal tenant
// tak pernah terlihat runner) baru muncul di runtime, jauh dari penyebabnya.
func TestResidensi_SchedulerHanyaDiJalurSentral(t *testing.T) {
	tenant, err := schema.CoreMigrations()
	if err != nil {
		t.Fatalf("CoreMigrations: %v", err)
	}
	central, err := schema.CentralMigrations()
	if err != nil {
		t.Fatalf("CentralMigrations: %v", err)
	}

	for _, m := range tenant {
		if m.Module == coreSched.MigrationModule {
			t.Errorf("migrasi scheduler %s:%s ada di jalur TENANT — ADR-023 menempatkannya di DB sentral",
				m.Module, m.Version)
		}
	}
	if len(central) == 0 {
		t.Fatal("jalur sentral kosong: migrasi scheduler tak akan pernah diterapkan ke DB mana pun")
	}
	for _, m := range central {
		if m.Module != coreSched.MigrationModule {
			t.Errorf("modul %q ada di jalur SENTRAL tanpa keputusan residensi — tambahkan ADR sebelum memindahkannya",
				m.Module)
		}
	}
}

// TestResidensi_TiapModulTepatSatuJalur menutup kesalahan tulis yang paling mudah terjadi saat
// memindahkan komponen: menyalin baris ke daftar baru tanpa menghapusnya dari yang lama. Modul
// yang ada di KEDUA jalur akan dibuat di dua DB, dan setengah pembacanya akan melihat tabel yang
// selalu kosong.
func TestResidensi_TiapModulTepatSatuJalur(t *testing.T) {
	tenant, err := schema.CoreMigrations()
	if err != nil {
		t.Fatalf("CoreMigrations: %v", err)
	}
	central, err := schema.CentralMigrations()
	if err != nil {
		t.Fatalf("CentralMigrations: %v", err)
	}

	di := map[string]string{}
	for _, m := range tenant {
		di[m.Module] = "tenant"
	}
	var ganda []string
	for _, m := range central {
		if jalur, ada := di[m.Module]; ada {
			ganda = append(ganda, m.Module+" (juga di "+jalur+")")
		}
	}
	if len(ganda) > 0 {
		t.Fatalf("modul ada di dua jalur residensi sekaligus: %s", strings.Join(ganda, ", "))
	}
}
