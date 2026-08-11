package main

import (
	"math"
	"testing"

	"github.com/huda-salam/pamong/core/config"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
)

// TestMaxHeaderBytes_SelaluMuatTokenYangLolosPagar adalah invariant koherensi antara dua batas
// yang mudah menyimpang: pagar ukuran token (ADR-020) dan MaxHeaderBytes server. Token yang
// dinyatakan SAH oleh pagar harus bisa dikirim balik sebagai "Authorization: Bearer …", jika
// tidak aplikasi sendiri menjadi pagar kedua yang menolak token yang baru saja diterbitkannya —
// kegagalan yang sama membingungkannya dengan yang hendak dicegah, hanya berpindah tempat.
func TestMaxHeaderBytes_SelaluMuatTokenYangLolosPagar(t *testing.T) {
	// MaxHeaderBytes adalah anggaran TOTAL (request line + semua header), bukan per header. Jadi
	// yang harus tersisa bukan cuma prefiks "Authorization: Bearer " + CRLF, melainkan itu plus
	// ruang untuk header lain di request yang sama: Cookie beberapa KiB, Referer, User-Agent,
	// header trace. Bila anggaran total hanya sebesar token + prefiks, request yang PROXY-nya
	// loloskan akan dijawab 431 oleh aplikasi sendiri.
	const (
		bearerOverhead = len("Authorization: Bearer ") + 2
		headerLain     = 8 << 10 // batas bawah yang wajar untuk sisa header sebuah request browser
	)

	for _, tokenMax := range []int{0, 1024, identitytoken.DefaultMaxBytes, 16000, config.MaxTokenMaxBytes} {
		got := maxHeaderBytes(tokenMax)

		effective := tokenMax
		if effective <= 0 {
			effective = identitytoken.DefaultMaxBytes
		}
		if effective > config.MaxTokenMaxBytes {
			effective = config.MaxTokenMaxBytes
		}
		if got < effective+bearerOverhead+headerLain {
			t.Errorf("maxHeaderBytes(%d) = %d — anggaran TOTAL tak memuat token %d byte + %d byte "+
				"prefiks Bearer + %d byte header lain",
				tokenMax, got, effective, bearerOverhead, headerLain)
		}
		// Batas tetap harus jauh di bawah default Go 1 MiB: itu setengah alasan ia dinyatakan
		// eksplisit (satu klien tak boleh bisa menahan 1 MiB buffer per koneksi).
		if got >= 1<<20 {
			t.Errorf("maxHeaderBytes(%d) = %d — tak lebih ketat dari default Go 1 MiB", tokenMax, got)
		}
	}
}

// TestMaxHeaderBytes_LantaiTidakLebihKetatDariProxy: pada konfigurasi bawaan, anggaran total harus
// melampaui batas PER-HEADER proxy yang lazim (nginx 8 KiB, ALB 16 KiB) — sebuah request bisa
// membawa beberapa header besar sekaligus. Lebih ketat dari itu membuat aplikasi menolak request
// yang proxy-nya justru meloloskan: permukaan kegagalan baru, bukan perlindungan.
func TestMaxHeaderBytes_LantaiTidakLebihKetatDariProxy(t *testing.T) {
	if got, want := maxHeaderBytes(0), 16<<10; got <= want {
		t.Fatalf("maxHeaderBytes(default) = %d, harus di ATAS batas per-header ALB %d", got, want)
	}
}

// TestMaxHeaderBytes_NilaiEkstrem_TidakMeluap: config.Validate menolak ambang di atas plafon, tapi
// fungsi ini tak boleh bergantung pada itu — pemanggil lain (test, tooling) bisa memberi angka apa
// pun. Tanpa kurungan, `tokenMaxBytes + slack` MELUAP jadi negatif dan batas header jatuh ke floor
// sementara pagar token praktis mati: aplikasi lalu menolak token yang ia sendiri terbitkan.
func TestMaxHeaderBytes_NilaiEkstrem_TidakMeluap(t *testing.T) {
	for _, tokenMax := range []int{config.MaxTokenMaxBytes + 1, 1 << 30, math.MaxInt} {
		got := maxHeaderBytes(tokenMax)
		if got <= 0 {
			t.Fatalf("maxHeaderBytes(%d) = %d — meluap/negatif", tokenMax, got)
		}
		if got >= 1<<20 {
			t.Fatalf("maxHeaderBytes(%d) = %d — tak lebih ketat dari default Go 1 MiB", tokenMax, got)
		}
		// Nilai ekstrem dikurung ke plafon config, jadi hasilnya harus sama dengan plafon itu.
		if want := maxHeaderBytes(config.MaxTokenMaxBytes); got != want {
			t.Fatalf("maxHeaderBytes(%d) = %d, mau dikurung ke %d", tokenMax, got, want)
		}
	}
}
