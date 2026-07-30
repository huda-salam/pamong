package crypto

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/huda-salam/pamong/core/config"
)

// Custody menyatakan SIAPA yang memegang KEK untuk sebuah tenant (ADR-010 §3). Ini kebijakan
// per-tenant (kolom id.tenant_registry.key_custody), bukan invariant framework: tiap pemda
// bisa punya kontrak berbeda.
type Custody string

const (
	// CustodyPlatform — KEK di KeyProvider yang dikelola platform. Default Tier 1/2.
	CustodyPlatform Custody = "platform"
	// CustodyTenant — KEK di KeyProvider milik pemda (mis. Vault/HSM pemda, Tier 3).
	// Belum didukung di PR-3.8.2; lihat ErrCustodyUnsupported.
	CustodyTenant Custody = "tenant"
)

// KeyKind memisahkan kunci enkripsi dari kunci blind index. Keduanya SENGAJA kunci berbeda,
// bukan turunan dari satu DEK: rotasi kunci enkripsi murah & lazy, sedangkan rotasi kunci
// blind index memaksa reindex seluruh baris (ADR-009 §2). Menurunkan keduanya dari satu DEK
// akan menyeret reindex mahal setiap kali kunci enkripsi dirotasi.
type KeyKind string

const (
	KindEncryption KeyKind = "enc"
	KindBlindIndex KeyKind = "bidx"
)

// dekLen adalah panjang DEK: 32 byte melayani keduanya — AES-256 (enkripsi) dan kunci
// HMAC-SHA256 (blind index).
const dekLen = 32

// ErrCustodyUnsupported dikembalikan saat sebuah tenant meminta mode custody yang belum
// punya KeyProvider terdaftar. GAGAL LANTANG dengan sengaja: jatuh diam-diam ke provider
// platform akan memberi jaminan kedaulatan kunci yang tidak benar kepada pemda yang justru
// memilih memegang kuncinya sendiri.
var ErrCustodyUnsupported = errors.New("crypto: mode custody belum didukung (tak ada KeyProvider terdaftar)")

// KeyRef mengidentifikasi satu kunci secara VENDOR-AGNOSTIK (ADR-010 §2). Pemetaan KeyRef →
// lokasi kunci nyata adalah urusan masing-masing driver, sehingga menambah KMS baru tidak
// mengubah kode kripto maupun pemanggilnya.
type KeyRef struct {
	TenantID string
	Purpose  string
	Kind     KeyKind
	Custody  Custody
}

// KeyProvider adalah abstraksi KMS: ia membungkus & membuka DEK, dan TIDAK PERNAH
// membocorkan KEK (ADR-010 §1). Menambah KMS = satu implementasi + RegisterProvider.
type KeyProvider interface {
	// GenerateDEK membuat DEK baru (acak kriptografis) sekaligus versi ter-wrap-nya.
	GenerateDEK(ctx context.Context, ref KeyRef) (plain, wrapped []byte, err error)
	// WrapDEK membungkus DEK dengan KEK milik ref.
	WrapDEK(ctx context.Context, ref KeyRef, dek []byte) ([]byte, error)
	// UnwrapDEK membuka DEK ter-wrap. Wajib gagal bila wrapped dibuat untuk KeyRef lain
	// (mis. tenant berbeda) — pengikatan ini yang menjaga isolasi antar tenant.
	UnwrapDEK(ctx context.Context, ref KeyRef, wrapped []byte) ([]byte, error)
}

// ProviderFactory membangun KeyProvider dari config. Signature-nya sengaja hanya menerima
// CryptoConfig: driver membaca field yang relevan untuknya (endpoint, master key, ...).
type ProviderFactory func(cfg config.CryptoConfig) (KeyProvider, error)

var (
	providersMu sync.RWMutex
	providers   = map[string]ProviderFactory{}
)

// RegisterProvider mendaftarkan driver KMS (titik ekstensi #1). Driver eksternal
// (vault/aws-kms/bssn) memanggil ini dari init()-nya sendiri; menambah KMS tidak menyentuh
// kode kripto. Panic saat nama ganda — kesalahan program, harus ketahuan saat boot.
func RegisterProvider(name string, f ProviderFactory) {
	if name == "" || f == nil {
		panic("crypto: RegisterProvider butuh nama & factory")
	}
	providersMu.Lock()
	defer providersMu.Unlock()
	if _, dup := providers[name]; dup {
		panic(fmt.Sprintf("crypto: driver KMS %q sudah terdaftar", name))
	}
	providers[name] = f
}

// NewProvider membangun KeyProvider driver bernama name. Nama tak dikenal = error yang
// menyebut pilihan yang ada (typo config gagal saat boot, bukan saat baris pertama ditulis).
func NewProvider(name string, cfg config.CryptoConfig) (KeyProvider, error) {
	providersMu.RLock()
	f, ok := providers[name]
	providersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("crypto: driver KMS tidak dikenal: %q (terdaftar: %s)", name, strings.Join(RegisteredProviders(), "|"))
	}
	return f(cfg)
}

// RegisteredProviders mengembalikan nama driver terdaftar (terurut) — untuk pesan error
// & diagnosa boot.
func RegisteredProviders() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
