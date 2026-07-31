package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// NewRepository adalah factory repository yang menerapkan kebijakan audit EntityDef
// secara otomatis (PR-1.3.3 auto-attach): entity Audited dibungkus pencatat audit,
// entity NotAudited memakai repo polos tanpa overhead. Modul TIDAK menulis kode audit —
// cukup mendeklarasikan Audited{} di EntityDef dan memakai repo dari factory ini.
// Bila entity punya field terenkripsi (ADR-009), serahkan CryptoPort lewat WithCrypto:
// spec kripto DITURUNKAN dari EntityDef di sini, sehingga kolom yang dideklarasikan
// personal_id/specific pasti ikut terenkripsi tanpa developer modul menulis apa pun.
// Tanpa CryptoPort, entity ber-field terenkripsi DITOLAK — menyimpannya plaintext karena
// wiring lupa dipasang adalah kegagalan senyap yang tak boleh dibiarkan.
func NewRepository[T any](pool *Pool, m Mapper[T], def domain.EntityDef, engine *audit.Engine, opts ...RepoOption) (port.BaseRepository[T], error) {
	specs := FieldCryptoFromEntity(def)

	var o repoOptions
	for _, opt := range opts {
		opt(&o)
	}
	if len(specs) > 0 {
		if o.crypto == nil {
			return nil, fmt.Errorf(
				"entity %q punya field terenkripsi (%d kolom) tapi CryptoPort tidak diberikan — pakai db.WithCrypto(...)",
				def.Name, len(specs))
		}
		// Salin dulu: append langsung ke slice variadic bisa menulis ke backing array milik
		// pemanggil bila ia meneruskan slice-nya sendiri dengan kapasitas sisa.
		opts = append(append(make([]RepoOption, 0, len(opts)+1), opts...), WithFieldCrypto(o.crypto, specs))
	}

	base, err := NewSQLRepository[T](pool, m, opts...)
	if err != nil {
		return nil, err
	}
	if !def.IsAudited() {
		return base, nil
	}
	if engine == nil {
		return nil, fmt.Errorf("entity %q Audited tapi audit engine nil", def.Name)
	}
	return &auditedRepo[T]{
		inner:   base,
		mapper:  m,
		engine:  engine,
		entity:  def.Schema + "." + def.Name,
		crypto:  o.crypto,
		diffEnc: specs,
	}, nil
}

// WithCrypto menyerahkan CryptoPort untuk entity ber-field terenkripsi. Spec kolomnya
// diturunkan dari EntityDef — pemanggil tidak menyusunnya sendiri.
func WithCrypto(c port.CryptoPort) RepoOption {
	return func(o *repoOptions) { o.crypto = c }
}

// auditedRepo membungkus SQLRepository dan mencatat audit pada setiap mutasi.
// Operasi baca (FindByID, List) diteruskan apa adanya tanpa audit.
type auditedRepo[T any] struct {
	inner  *SQLRepository[T]
	mapper Mapper[T]
	engine *audit.Engine
	entity string // "schema.Entity"

	// crypto & diffEnc menutup jalur kebocoran samping #1 ADR-009 §6: snapshot audit diambil
	// dari entity (nilai PLAINTEXT), bukan dari kolom DB. Tanpa ini, mengenkripsi kolom hanya
	// memindahkan kebocoran ke gov.audit_logs.diff — nilai raw tetap tersimpan sebagai bukti
	// (keputusan ADR-002 tak berubah), tapi dalam bentuk terenkripsi.
	crypto  port.CryptoPort
	diffEnc []FieldCryptoSpec
}

var _ port.BaseRepository[struct{}] = (*auditedRepo[struct{}])(nil)

func (r *auditedRepo[T]) FindByID(ctx context.Context, id uuid.UUID) (*T, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *auditedRepo[T]) List(ctx context.Context, f port.ListFilter) (*port.ListResult[T], error) {
	return r.inner.List(ctx, f)
}

func (r *auditedRepo[T]) Save(ctx context.Context, entity *T) error {
	if err := r.inner.Save(ctx, entity); err != nil {
		return err
	}
	after := r.snapshot(entity)
	r.sealPair(ctx, nil, after)
	return r.record(ctx, audit.ActionCreate, r.mapper.ID(entity), nil, after)
}

func (r *auditedRepo[T]) Update(ctx context.Context, entity *T) error {
	before, err := r.inner.FindByID(ctx, r.mapper.ID(entity))
	if err != nil {
		return err
	}
	if err := r.inner.Update(ctx, entity); err != nil {
		return err
	}
	b, a := r.snapshot(before), r.snapshot(entity)
	r.sealPair(ctx, b, a)
	return r.record(ctx, audit.ActionUpdate, r.mapper.ID(entity), b, a)
}

func (r *auditedRepo[T]) SoftDelete(ctx context.Context, id uuid.UUID) error {
	before, err := r.inner.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.inner.SoftDelete(ctx, id); err != nil {
		return err
	}
	b := r.snapshot(before)
	r.sealPair(ctx, b, nil)
	return r.record(ctx, audit.ActionDelete, id, b, nil)
}

// snapshot membangun map kolom bisnis entity (lewat Mapper). Nilainya masih PLAINTEXT —
// penyegelan terjadi belakangan, di sealPair, karena perbandingan before/after harus
// mendahului enkripsi.
func (r *auditedRepo[T]) snapshot(e *T) map[string]any {
	cols := r.mapper.DataColumns()
	vals := r.mapper.DataValues(e)
	out := make(map[string]any, len(cols))
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}

// sealPair mengganti nilai kolom terenkripsi pada KEDUA sisi snapshot dengan ciphertext
// base64, sehingga diff tetap menjadi bukti before/after bagi pemeriksa (ADR-002) tapi tak
// terbaca tanpa kunci. Sisi yang tidak ada (create/delete) dilewati.
//
// Kedua sisi diproses bersama, dan itu bukan kerapian belaka: Encrypt memakai nonce acak,
// jadi satu nilai plaintext yang sama menghasilkan ciphertext berbeda tiap panggilan. Bila
// tiap sisi disegel sendiri-sendiri, audit.Diff (reflect.DeepEqual) melaporkan SETIAP kolom
// terenkripsi sebagai berubah pada SETIAP update — jejak audit mengarang perubahan pengenal
// dan supresi no-op update ikut mati. Karena itu nilai yang tidak berubah disegel SEKALI
// lalu dipasang di kedua sisi (Diff membuangnya), yang berubah disegel per sisi.
func (r *auditedRepo[T]) sealPair(ctx context.Context, before, after map[string]any) {
	if len(r.diffEnc) == 0 || r.crypto == nil {
		return
	}
	tenantID := port.TenantFrom(ctx)
	for _, s := range r.diffEnc {
		purpose := s.Purpose
		if purpose == "" {
			purpose = s.Column
		}
		bRaw, bOK := before[s.Column]
		aRaw, aOK := after[s.Column]
		bPlain, bNull, bErr := plaintextOf(s.Column, bRaw)
		aPlain, aNull, aErr := plaintextOf(s.Column, aRaw)

		if bOK && aOK && bErr == nil && aErr == nil && bNull == aNull && bytes.Equal(bPlain, aPlain) {
			v := r.seal(ctx, tenantID, purpose, bPlain, bNull, nil, auditRedacted)
			before[s.Column], after[s.Column] = v, v
			continue
		}
		if bOK {
			before[s.Column] = r.seal(ctx, tenantID, purpose, bPlain, bNull, bErr, auditRedactedBefore)
		}
		if aOK {
			after[s.Column] = r.seal(ctx, tenantID, purpose, aPlain, aNull, aErr, auditRedactedAfter)
		}
	}
}

// seal menyegel satu nilai. Kegagalan TIDAK membatalkan pencatatan — nilainya diganti
// penanda, karena kehilangan satu nilai lebih baik daripada menyimpan pengenal mentah.
func (r *auditedRepo[T]) seal(ctx context.Context, tenantID, purpose string, plain []byte, isNull bool, parseErr error, marker string) any {
	if parseErr == nil && isNull {
		return nil
	}
	if parseErr != nil || tenantID == "" {
		return marker
	}
	ct, err := r.crypto.Encrypt(ctx, tenantID, purpose, plain)
	if err != nil {
		return marker
	}
	return base64.StdEncoding.EncodeToString(ct)
}

// Penanda nilai sensitif yang gagal disegel (mis. tenant tak diketahui atau KMS sedang
// gagal). Sengaja mencolok agar kondisi ini terlihat saat audit dibaca alih-alih menyamar
// sebagai nilai kosong.
//
// Penanda before & after WAJIB berbeda: penanda yang sama di kedua sisi membuat perubahan
// nyata (A→B) tampak tidak berubah bagi Diff lalu terbuang — dan bila itu satu-satunya field
// yang berubah, mutasi ter-commit tanpa entry audit sama sekali. Penanda tunggal hanya boleh
// dipakai untuk nilai yang memang terbukti sama di kedua sisi.
const (
	auditRedacted       = "[terenkripsi: gagal — nilai tidak dicatat]"
	auditRedactedBefore = "[terenkripsi: gagal — nilai lama tidak dicatat]"
	auditRedactedAfter  = "[terenkripsi: gagal — nilai baru tidak dicatat]"
)

// record menyusun konteks audit. Mutasi entity Auditable wajib punya AuthContext
// (actor + tenant) — caller use case selalu meneruskannya.
func (r *auditedRepo[T]) record(ctx context.Context, action audit.Action, id uuid.UUID, before, after map[string]any) error {
	actx, ok := ctx.(port.AuthContext)
	if !ok {
		return core.ErrValidation("ctx", "mutasi entity Auditable butuh AuthContext (actor tak diketahui)")
	}
	return r.engine.Record(actx, audit.RecordInput{
		Entity:   r.entity,
		EntityID: id,
		Action:   action,
		Before:   before,
		After:    after,
	})
}
