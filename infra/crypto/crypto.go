// Package crypto adalah driven adapter yang mengimplementasi port.CryptoPort (ADR-009):
// enkripsi field selektif at-rest (AES-256-GCM) + blind index (HMAC-SHA256) agar equality
// lookup & UNIQUE tetap bekerja, di atas envelope encryption KEK→DEK per-tenant per-purpose
// (ADR-010).
//
// SENSITIF. Batas yang tidak boleh dilanggar:
//   - Kunci mentah tidak pernah ditulis ke DB tenant maupun di-log/trace.
//   - Dekripsi selalu ber-tenantID (hierarki DEK per-tenant).
//   - Dipanggil dari lapis repository (infra/db), BUKAN dari use case/domain.
//
// Status PR-3.8.2: paket ini lengkap & teruji, tapi BELUM di-wire ke repository — pemanggilan
// otomatis dari FieldDef.Class adalah PR-3.8.3. Karena itu tak ada perubahan perilaku pada
// jalur data yang sudah jalan.
package crypto

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
)

// Format ciphertext field (ADR-009 §3) — self-describing agar rotasi kunci tidak butuh
// migrasi data, dan agar Decrypt tak perlu diberi purpose:
//
//	byte 0        : versi format (ctFormatV2)
//	byte 1        : panjang purpose (1..255)
//	byte 2..      : purpose (key_id: konteks kunci, mis. "nik")
//	4 byte  (BE)  : key_version — DEK mana yang membukanya
//	12 byte       : nonce GCM (acak per-nilai)
//	sisanya       : ciphertext + tag GCM
//
// record_id (ADR-016) TIDAK ada di dalam blob — hanya di AAD. Menyimpannya di sini akan
// membatalkan gunanya: blob yang dipindah akan membawa serta "bukti" identitasnya sendiri.
//
// v1 (pra-ADR-016) memakai tata letak yang sama persis tapi AAD tanpa record_id. Ia masih
// DIKENALI parser — sehingga PurposeOf menjawab dan jalur baca audit bisa menampilkan
// penanda "tidak dapat dibuka" alih-alih blob mentah — tapi DITOLAK Decrypt: menerimanya
// sama dengan menerima ciphertext tak terikat baris.
const (
	ctFormatV1     = 0x01
	ctFormatV2     = 0x02
	ctVersionBytes = 4
)

// CustodyProvider mendaftarkan satu KeyProvider untuk satu mode custody. Menambah dukungan
// custody baru (mis. Vault milik pemda pada Tier 3, PR-3.8.8) = menyerahkan satu entri lagi
// ke New — tanpa mengubah kode enkripsi maupun port.CryptoPort.
type CustodyProvider struct {
	Custody  Custody
	Driver   string // nama driver KMS; tersimpan di kolom kek_driver untuk diagnosa
	Provider KeyProvider
}

// Service mengimplementasi port.CryptoPort.
type Service struct {
	keys *keyManager
}

var _ port.CryptoPort = (*Service)(nil)

// New merakit Service dari store DEK, resolver custody, dan daftar provider per-custody.
// Minimal satu CustodyProvider wajib — Service tanpa provider tak bisa melakukan apa pun,
// dan gagal saat konstruksi jauh lebih baik daripada gagal saat baris pertama ditulis.
func New(store DEKStore, custody CustodyResolver, ttl time.Duration, provs ...CustodyProvider) (*Service, error) {
	if store == nil || custody == nil {
		return nil, fmt.Errorf("crypto: New butuh DEKStore & CustodyResolver")
	}
	if len(provs) == 0 {
		return nil, fmt.Errorf("crypto: New butuh minimal satu CustodyProvider")
	}
	providers := make(map[Custody]namedProvider, len(provs))
	for _, p := range provs {
		if p.Provider == nil || p.Driver == "" || p.Custody == "" {
			return nil, fmt.Errorf("crypto: CustodyProvider tidak lengkap (custody=%q driver=%q)", p.Custody, p.Driver)
		}
		if _, dup := providers[p.Custody]; dup {
			return nil, fmt.Errorf("crypto: custody %q didaftarkan dua kali", p.Custody)
		}
		providers[p.Custody] = namedProvider{name: p.Driver, provider: p.Provider}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Service{keys: newKeyManager(store, custody, providers, ttl)}, nil
}

// NewFromConfig merakit Service produksi: DEK store & registry tenant di identity DB
// (sentral), driver KMS dari config.
//
// identityConn WAJIB koneksi identity DB — di sanalah id.data_keys & id.tenant_registry
// hidup (ADR-010 §2: kunci tak boleh ada di tenant DB).
//
// PR-3.8.2 memasang provider untuk custody `platform` saja. Tenant ber-key_custody='tenant'
// akan ditolak lantang (ErrCustodyUnsupported) sampai driver pemda didaftarkan di PR-3.8.8 —
// penambahan itu tidak menyentuh kode kripto, cukup satu CustodyProvider tambahan di sini.
func NewFromConfig(cfg *config.AppConfig, identityConn port.DBConn) (*Service, error) {
	if cfg == nil || identityConn == nil {
		return nil, fmt.Errorf("crypto: NewFromConfig butuh config & koneksi identity DB")
	}
	driver := cfg.Crypto.KMSDriver
	if driver == "" {
		driver = DriverLocal
	}
	// Gerbang kedua atas driver dev (yang pertama di config.Validate): kunci driver `local`
	// ada di source code, jadi ia tak boleh menyentuh data mirip-nyata di staging/production.
	if driver == DriverLocal && cfg.Env != "development" {
		return nil, fmt.Errorf("crypto: driver %q hanya untuk development (env=%q butuh %q atau KMS nyata)",
			DriverLocal, cfg.Env, DriverStatic)
	}

	provider, err := NewProvider(driver, cfg.Crypto)
	if err != nil {
		return nil, err
	}
	ttl := cfg.Crypto.DEKCacheTTLOrDefault()
	return New(
		NewDBDEKStore(identityConn),
		NewDBCustodyResolver(identityConn, ttl),
		ttl,
		CustodyProvider{Custody: CustodyPlatform, Driver: driver, Provider: provider},
	)
}

// Encrypt — lihat port.CryptoPort. Nonce acak per-nilai: dua panggilan atas plaintext sama
// menghasilkan ciphertext berbeda (karena itu equality memakai BlindIndex, bukan kolom _enc).
func (s *Service) Encrypt(ctx context.Context, ref port.FieldRef, plain []byte) ([]byte, error) {
	if err := validateFieldRef(ref); err != nil {
		return nil, err
	}
	version, dek, err := s.keys.ActiveKey(ctx, ref.TenantID, ref.Purpose, KindEncryption)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	nonce := make([]byte, gcmNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: gagal membangkitkan nonce: %w", err)
	}

	header := make([]byte, 0, 2+len(ref.Purpose)+ctVersionBytes+gcmNonceLen)
	header = append(header, ctFormatV2, byte(len(ref.Purpose)))
	header = append(header, ref.Purpose...)
	header = binary.BigEndian.AppendUint32(header, uint32(version))
	header = append(header, nonce...)

	return gcm.Seal(header, nonce, plain, fieldAAD(ref, version)), nil
}

// Decrypt — lihat port.CryptoPort. Ciphertext milik tenant lain ATAU baris lain gagal
// dibuka meski DEK-nya tersedia di proses ini: keduanya ikut ke dalam AAD (ADR-016).
func (s *Service) Decrypt(ctx context.Context, ref port.RowRef, ct []byte) ([]byte, error) {
	if err := validateRowRef(ref); err != nil {
		return nil, err
	}
	format, purpose, version, nonce, payload, err := parseCiphertext(ct)
	if err != nil {
		return nil, err
	}
	// Blob pra-ADR-016 dikenali agar operator tahu yang dibutuhkan adalah RE-ENKRIPSI,
	// bukan menebak antara "blob rusak" dan "kunci salah". Ia tetap ditolak: menerimanya
	// berarti membiarkan celah pemindahan antar baris terbuka lewat pintu kompatibilitas.
	if format == ctFormatV1 {
		return nil, fmt.Errorf(
			"%w: ciphertext format v1 (pra-pengikatan baris, ADR-016) — butuh re-enkripsi, purpose %q",
			port.ErrCiphertextInvalid, purpose)
	}
	dek, err := s.keys.KeyByVersion(ctx, ref.TenantID, purpose, KindEncryption, version)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	aad := fieldAAD(port.FieldRef{TenantID: ref.TenantID, Purpose: purpose, RecordID: ref.RecordID}, version)
	plain, err := gcm.Open(nil, nonce, payload, aad)
	if err != nil {
		// Satu jawaban untuk semua sebab (kunci salah, tenant salah, BARIS salah, blob
		// dimodifikasi): membedakannya hanya akan membantu penyerang.
		return nil, fmt.Errorf("%w: gagal dibuka untuk tenant %q purpose %q", port.ErrCiphertextInvalid, ref.TenantID, purpose)
	}
	return plain, nil
}

// BlindIndex — lihat port.CryptoPort. Deterministik: inilah penopang lookup equality &
// UNIQUE pada kolom _bidx.
//
// Kuncinya KindBlindIndex, terpisah dari kunci enkripsi. Versi kunci TIDAK dibawa nilai
// bidx (tak ada tempatnya — kolom itu dibandingkan apa adanya), sehingga merotasi kunci
// blind index memaksa reindex seluruh baris. Itu operasi kompromi, bukan rutin (ADR-009 §2).
func (s *Service) BlindIndex(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error) {
	if err := validateRefInput(tenantID, purpose); err != nil {
		return nil, err
	}
	_, key, err := s.keys.ActiveKey(ctx, tenantID, purpose, KindBlindIndex)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(normalize(purpose, plain))
	return mac.Sum(nil), nil
}

// fieldAAD mengikat ciphertext ke (tenant, purpose, versi kunci, BARIS) — ADR-016 §1.
//
// Dari keempatnya, tenantID & recordID datang dari LUAR blob (diberikan pemanggil);
// purpose & version dibaca DARI blob itu sendiri, supaya Decrypt tak perlu diberi purpose
// dan rotasi kunci tak butuh migrasi format (ADR-009 §3). Justru komponen yang disuplai
// pemanggil-lah yang menegakkan sesuatu: blob yang dipindah ke tenant/baris lain akan
// diminta dibuka dengan koordinat tujuan, yang bukan koordinat saat ia disegel.
//
// BATAS YANG TETAP ADA: AAD ini TIDAK mengikat KOLOM. Memindahkan `nik_enc` ke
// `no_rekening_enc` pada BARIS YANG SAMA menghasilkan AAD yang identik dan tetap terbuka.
// Penegakan kolom ada di lapis pemanggil: repository WAJIB membandingkan PurposeOf(ct)
// dengan purpose kolom yang sedang dibaca (ADR-015). Lihat PurposeOf.
//
// Setiap komponen ditulis ber-length-prefix, bukan digabung dengan pemisah: recordID tak
// selamanya UUID (jalur idempotency ber-kunci string dari klien), dan nilai yang memuat
// karakter pemisah bisa menghasilkan AAD yang sama untuk dua koordinat berbeda.
func fieldAAD(ref port.FieldRef, version int) []byte {
	out := make([]byte, 0, 64+len(ref.TenantID)+len(ref.Purpose)+len(ref.RecordID))
	out = append(out, aadPrefix...)
	for _, part := range []string{ref.TenantID, ref.Purpose, ref.RecordID} {
		out = binary.BigEndian.AppendUint32(out, uint32(len(part)))
		out = append(out, part...)
	}
	return binary.BigEndian.AppendUint32(out, uint32(version))
}

// aadPrefix menandai domain AAD. Ia ikut naik versi bersama format ciphertext supaya blob
// v1 tak akan pernah kebetulan cocok dengan AAD v2.
const aadPrefix = "pamong/field/v2"

// PurposeOf — lihat port.CryptoPort. Membaca purpose dari header ciphertext tanpa kunci
// maupun I/O, sehingga lapis repository bisa menegakkan pengikatan kolom yang TIDAK bisa
// ditegakkan AAD (lihat fieldAAD).
//
// Metode pada Service (bukan sekadar fungsi paket) supaya pemanggil cukup bergantung pada
// port.CryptoPort: pemeriksaan kolom tak boleh mengasumsikan format milik satu implementasi.
func (s *Service) PurposeOf(ct []byte) (string, error) {
	return PurposeOf(ct)
}

// PurposeOf versi fungsi paket — dipakai internal & alat baris perintah yang hanya punya blob.
//
// Sengaja MENJAWAB juga untuk blob v1 (pra-ADR-016): pemeriksaan pengikatan kolom di repo
// dan pengenalan "nilai ini sensitif" di jalur baca audit harus tetap bekerja atas data
// lama, supaya blob lama berakhir sebagai penanda "tidak dapat dibuka" — bukan tampil
// sebagai nilai biasa. Penolakannya terjadi di Decrypt, bukan di sini.
func PurposeOf(ct []byte) (string, error) {
	_, purpose, _, _, _, err := parseCiphertext(ct)
	return purpose, err
}

func parseCiphertext(ct []byte) (format byte, purpose string, version int, nonce, payload []byte, err error) {
	// Minimal: header(2) + purpose(≥1) + version(4) + nonce(12) + tag(16).
	if len(ct) < 2 {
		return 0, "", 0, nil, nil, fmt.Errorf("%w: terlalu pendek (%d byte)", port.ErrCiphertextInvalid, len(ct))
	}
	if ct[0] != ctFormatV1 && ct[0] != ctFormatV2 {
		return 0, "", 0, nil, nil, fmt.Errorf("%w: versi format tak dikenal (%#x)", port.ErrCiphertextInvalid, ct[0])
	}
	purposeLen := int(ct[1])
	if purposeLen == 0 {
		return 0, "", 0, nil, nil, fmt.Errorf("%w: purpose kosong", port.ErrCiphertextInvalid)
	}
	head := 2 + purposeLen + ctVersionBytes + gcmNonceLen
	if len(ct) <= head {
		return 0, "", 0, nil, nil, fmt.Errorf("%w: header terpotong", port.ErrCiphertextInvalid)
	}
	purpose = string(ct[2 : 2+purposeLen])
	version = int(binary.BigEndian.Uint32(ct[2+purposeLen : 2+purposeLen+ctVersionBytes]))
	nonce = ct[2+purposeLen+ctVersionBytes : head]
	payload = ct[head:]
	return ct[0], purpose, version, nonce, payload, nil
}

// purposeMaxLen mengikuti batas satu byte panjang di header ciphertext; nama purpose adalah
// nama field/konteks yang ditentukan framework, jadi batas ini longgar.
const purposeMaxLen = 255

func validateRefInput(tenantID, purpose string) error {
	if tenantID == "" {
		return fmt.Errorf("crypto: tenantID wajib (hierarki DEK per-tenant)")
	}
	if purpose == "" {
		return fmt.Errorf("crypto: purpose wajib (konteks kunci, mis. \"nik\")")
	}
	if len(purpose) > purposeMaxLen {
		return fmt.Errorf("crypto: purpose maksimal %d byte, bukan %d", purposeMaxLen, len(purpose))
	}
	return nil
}

// validateFieldRef & validateRowRef menjaga koordinat lengkap sebelum kripto berjalan.
// RecordID kosong GAGAL, tidak pernah diperlakukan sebagai nilai default: hasilnya akan
// menjadi nilai yang bisa dipindah ke baris mana pun, dan kegagalan itu tak terlihat
// sampai seseorang memindahkannya (ADR-016 §6).
func validateFieldRef(ref port.FieldRef) error {
	if err := validateRefInput(ref.TenantID, ref.Purpose); err != nil {
		return err
	}
	if ref.RecordID == "" {
		return fmt.Errorf("crypto: RecordID wajib (pengikatan baris, ADR-016)")
	}
	return nil
}

func validateRowRef(ref port.RowRef) error {
	if ref.TenantID == "" {
		return fmt.Errorf("crypto: Decrypt butuh tenantID (hierarki DEK per-tenant)")
	}
	if ref.RecordID == "" {
		return fmt.Errorf("crypto: Decrypt butuh RecordID (pengikatan baris, ADR-016)")
	}
	return nil
}

// caseFoldedPurposes adalah purpose yang nilainya setara tanpa memandang huruf besar/kecil —
// tabel KEBIJAKAN FRAMEWORK (ADR-009 §1), bukan pilihan per-modul. Salah menormalkan berarti
// UNIQUE tak menangkap duplikat ("Budi@x.id" vs "budi@x.id") atau lookup gagal menemukan
// baris yang ada. Menambah entri = satu baris di sini.
var caseFoldedPurposes = map[string]bool{
	"email": true,
}

// normalize menyiapkan nilai sebelum di-HMAC. Spasi tepi selalu dibuang: perbedaan yang tak
// terlihat mata tidak boleh membuat dua nilai sama menjadi dua indeks berbeda.
func normalize(purpose string, plain []byte) []byte {
	out := strings.TrimSpace(string(plain))
	if caseFoldedPurposes[purpose] {
		out = strings.ToLower(out)
	}
	return []byte(out)
}

func joinSorted(values []string) string {
	sort.Strings(values)
	return strings.Join(values, "|")
}
