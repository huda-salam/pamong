package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strings"
)

// kekWrapper adalah mesin envelope in-app yang dipakai driver bawaan (static & local):
// membungkus/membuka DEK dengan master KEK ber-versi memakai AES-256-GCM. Driver KMS
// eksternal (vault/aws-kms/bssn) TIDAK memakai ini — di sana wrap/unwrap terjadi di dalam
// KMS dan KEK tak pernah keluar. Keduanya tetap memenuhi KeyProvider yang sama.
type kekWrapper struct {
	driver string         // nama driver, untuk pesan error & kolom kek_driver
	keys   map[int][]byte // versi → master KEK (32 byte)
	active int            // versi yang dipakai untuk MEMBUNGKUS DEK baru
}

// Format DEK ter-wrap (self-describing agar rotasi KEK tidak butuh migrasi data):
//
//	byte 0    : versi format wrap (wrapFormatV1)
//	byte 1    : versi master KEK (1..255) — menentukan kunci mana yang membuka blob ini
//	byte 2..13: nonce GCM (12 byte, acak per-wrap)
//	byte 14.. : DEK terenkripsi + tag GCM
const (
	wrapFormatV1  = 0x01
	gcmNonceLen   = 12
	wrapHeaderLen = 2 + gcmNonceLen
)

// kekAAD mengikat blob ter-wrap ke KeyRef-nya. Tanpa ini, baris id.data_keys milik tenant A
// bisa dipindah ke baris tenant B dan tetap terbuka — isolasi per-tenant hilang. Pemisah "|"
// aman: tenant_id dibatasi [a-z0-9-] (identity/domain.Tenant) dan purpose ditentukan
// framework (nama field), bukan input pengguna.
func kekAAD(ref KeyRef) []byte {
	return []byte(strings.Join([]string{
		"pamong/kek/v1", ref.TenantID, ref.Purpose, string(ref.Kind), string(ref.Custody),
	}, "|"))
}

func (w kekWrapper) GenerateDEK(ctx context.Context, ref KeyRef) (plain, wrapped []byte, err error) {
	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, fmt.Errorf("crypto/%s: gagal membangkitkan DEK: %w", w.driver, err)
	}
	wrapped, err = w.WrapDEK(ctx, ref, dek)
	if err != nil {
		return nil, nil, err
	}
	return dek, wrapped, nil
}

func (w kekWrapper) WrapDEK(_ context.Context, ref KeyRef, dek []byte) ([]byte, error) {
	if len(dek) != dekLen {
		return nil, fmt.Errorf("crypto/%s: DEK harus %d byte, bukan %d", w.driver, dekLen, len(dek))
	}
	kek, ok := w.keys[w.active]
	if !ok {
		return nil, fmt.Errorf("crypto/%s: master KEK versi aktif (%d) tidak tersedia", w.driver, w.active)
	}
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, fmt.Errorf("crypto/%s: %w", w.driver, err)
	}
	nonce := make([]byte, gcmNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto/%s: gagal membangkitkan nonce: %w", w.driver, err)
	}
	out := make([]byte, 0, wrapHeaderLen+len(dek)+gcm.Overhead())
	out = append(out, wrapFormatV1, byte(w.active))
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, dek, kekAAD(ref)), nil
}

func (w kekWrapper) UnwrapDEK(_ context.Context, ref KeyRef, wrapped []byte) ([]byte, error) {
	if len(wrapped) <= wrapHeaderLen {
		return nil, fmt.Errorf("crypto/%s: blob DEK ter-wrap terpotong (%d byte)", w.driver, len(wrapped))
	}
	if wrapped[0] != wrapFormatV1 {
		return nil, fmt.Errorf("crypto/%s: versi format wrap tak dikenal: %#x", w.driver, wrapped[0])
	}
	version := int(wrapped[1])
	kek, ok := w.keys[version]
	if !ok {
		// Terjadi bila master key versi lama dihapus dari config setelah rotasi — DEK lama jadi
		// tak terbuka. Pesan ini menyebut versinya agar ops bisa memulihkan kuncinya.
		return nil, fmt.Errorf("crypto/%s: master KEK versi %d tidak tersedia (DEK ini dibungkus dengan versi tersebut)", w.driver, version)
	}
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, fmt.Errorf("crypto/%s: %w", w.driver, err)
	}
	nonce := wrapped[2:wrapHeaderLen]
	dek, err := gcm.Open(nil, nonce, wrapped[wrapHeaderLen:], kekAAD(ref))
	if err != nil {
		// Sengaja tidak menyebut sebab detail: gagal di sini berarti kunci salah ATAU blob
		// milik KeyRef lain (mis. tenant berbeda) — keduanya satu jawaban: tidak boleh dibuka.
		return nil, fmt.Errorf("crypto/%s: DEK gagal dibuka (kunci salah atau blob bukan milik %s/%s)", w.driver, ref.TenantID, ref.Purpose)
	}
	if len(dek) != dekLen {
		return nil, fmt.Errorf("crypto/%s: DEK hasil unwrap berukuran %d byte, bukan %d", w.driver, len(dek), dekLen)
	}
	return dek, nil
}

// activeVersion mengembalikan versi master KEK tertinggi yang tersedia. Aturan "versi
// tertinggi = aktif" membuat rotasi KEK cukup dengan MENAMBAH master key versi baru
// (V1 tetap ada untuk membuka DEK lama) — tanpa knob config tambahan yang bisa salah set.
func activeVersion(keys map[int][]byte) int {
	active := 0
	for v := range keys {
		if v > active {
			active = v
		}
	}
	return active
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("kunci AES tidak sah: %w", err)
	}
	return cipher.NewGCM(block)
}
