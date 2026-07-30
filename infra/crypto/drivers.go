package crypto

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/huda-salam/pamong/core/config"
)

// Driver KMS bawaan framework. Keduanya didaftarkan ke registry di init() sehingga
// NewProvider melihatnya lewat jalur yang sama dengan driver eksternal — tidak ada jalur
// istimewa untuk driver bawaan (titik ekstensi #1).
const (
	// DriverStatic — KMS-alike bawaan, DEFAULT PRODUKSI Tier 1/2 (ADR-010 §1). Master KEK
	// ber-versi dari secret store/env; envelope in-app; nol dependensi eksternal sehingga
	// pemda tanpa HSM/Vault tetap bisa mengenkripsi. Postur: keamanan turun ke perlindungan
	// master key (ops) — jauh di atas plaintext, dan naik ke HSM/Vault = ganti driver saja.
	DriverStatic = "static"
	// DriverLocal — dev/test SAJA: kunci turunan tetap yang ada di dalam kode ini, tanpa
	// rotasi. config.Validate MENOLAK driver ini di luar development; NewFromConfig menolak
	// sekali lagi (dua gerbang, sengaja).
	DriverLocal = "local"
)

// localKEKSeed menurunkan KEK dev yang tetap (deterministik) — kunci ini PUBLIK karena ada
// di source code. Itu justru sifat yang diinginkan untuk test: hasil enkripsi bisa dibuka
// lintas proses test tanpa menyiapkan secret apa pun.
const localKEKSeed = "pamong crypto local dev KEK — JANGAN dipakai untuk data nyata"

// ErrMasterKeyRequired dikembalikan driver static bila tak ada master key yang sah. Driver
// MENOLAK START, bukan menunggu sampai baris pertama dienkripsi.
var ErrMasterKeyRequired = errors.New("crypto/static: master KEK wajib (GOV_CRYPTO_MASTER_KEY, base64 32-byte)")

func init() {
	RegisterProvider(DriverStatic, newStaticProvider)
	RegisterProvider(DriverLocal, newLocalProvider)
}

// newStaticProvider membangun driver static dari master key ber-versi di config. Versi
// tertinggi yang terisi menjadi versi aktif; versi lama WAJIB tetap ada agar DEK yang sudah
// dibungkus dengannya masih bisa dibuka (rotasi KEK = re-wrap DEK, data tak disentuh).
func newStaticProvider(cfg config.CryptoConfig) (KeyProvider, error) {
	keys, err := cfg.MasterKeys()
	if err != nil {
		return nil, fmt.Errorf("crypto/static: %w", err)
	}
	if len(keys) == 0 {
		return nil, ErrMasterKeyRequired
	}
	return kekWrapper{driver: DriverStatic, keys: keys, active: activeVersion(keys)}, nil
}

// newLocalProvider membangun driver dev/test dengan satu versi KEK turunan tetap. Master key
// di config DIABAIKAN — driver ini memang bukan tempat kunci nyata.
func newLocalProvider(_ config.CryptoConfig) (KeyProvider, error) {
	seed := sha256.Sum256([]byte(localKEKSeed))
	return kekWrapper{driver: DriverLocal, keys: map[int][]byte{1: seed[:]}, active: 1}, nil
}
