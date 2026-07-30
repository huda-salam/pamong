package db

import (
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
		opts = append(opts, WithFieldCrypto(o.crypto, specs))
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
	return r.record(ctx, audit.ActionCreate, r.mapper.ID(entity), nil, r.fields(ctx, entity))
}

func (r *auditedRepo[T]) Update(ctx context.Context, entity *T) error {
	before, err := r.inner.FindByID(ctx, r.mapper.ID(entity))
	if err != nil {
		return err
	}
	if err := r.inner.Update(ctx, entity); err != nil {
		return err
	}
	return r.record(ctx, audit.ActionUpdate, r.mapper.ID(entity), r.fields(ctx, before), r.fields(ctx, entity))
}

func (r *auditedRepo[T]) SoftDelete(ctx context.Context, id uuid.UUID) error {
	before, err := r.inner.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.inner.SoftDelete(ctx, id); err != nil {
		return err
	}
	return r.record(ctx, audit.ActionDelete, id, r.fields(ctx, before), nil)
}

// fields membangun snapshot map dari kolom bisnis entity (lewat Mapper).
//
// Nilai kolom terenkripsi TIDAK pernah masuk snapshot dalam bentuk plaintext: ia diganti
// ciphertext (base64) sehingga diff tetap menjadi bukti before/after untuk pemeriksa
// (ADR-002) tapi tak terbaca tanpa kunci. Bila enkripsi gagal, nilainya diganti penanda
// kegagalan — audit TETAP tercatat, tapi tak pernah dengan pengenal mentah.
func (r *auditedRepo[T]) fields(ctx context.Context, e *T) map[string]any {
	cols := r.mapper.DataColumns()
	vals := r.mapper.DataValues(e)
	out := make(map[string]any, len(cols))
	for i, c := range cols {
		out[c] = vals[i]
	}
	if len(r.diffEnc) == 0 || r.crypto == nil {
		return out
	}

	tenantID := port.TenantFrom(ctx)
	for _, s := range r.diffEnc {
		raw, ok := out[s.Column]
		if !ok {
			continue
		}
		plain, isNull, err := plaintextOf(s.Column, raw)
		if isNull && err == nil {
			out[s.Column] = nil
			continue
		}
		purpose := s.Purpose
		if purpose == "" {
			purpose = s.Column
		}
		if err != nil || tenantID == "" {
			out[s.Column] = auditRedacted
			continue
		}
		ct, err := r.crypto.Encrypt(ctx, tenantID, purpose, plain)
		if err != nil {
			out[s.Column] = auditRedacted
			continue
		}
		out[s.Column] = base64.StdEncoding.EncodeToString(ct)
	}
	return out
}

// auditRedacted dipakai bila nilai sensitif tak bisa dienkripsi (mis. tenant tak diketahui).
// Sengaja menjadi penanda yang mencolok: lebih baik diff kehilangan satu nilai daripada
// menyimpan pengenal mentah, dan penanda ini membuat kondisi tersebut terlihat saat audit
// dibaca alih-alih menyamar sebagai nilai kosong.
const auditRedacted = "[terenkripsi: gagal — nilai tidak dicatat]"

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
