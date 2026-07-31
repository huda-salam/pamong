package audit

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// Jalur BACA audit ber-kontrol akses (ADR-002 + ADR-009 §6 butir 1, DoD PR-3.8.4).
//
// Keputusan ADR-002 — simpan nilai mentah, kendalikan saat BACA — hanya utuh bila ada
// lapis baca yang benar-benar mengendalikannya. Enkripsi diff (PR-3.8.4) membuat nilai
// pengenal tak terbaca tanpa kunci; Reader inilah yang menentukan SIAPA memperoleh kunci
// itu, sehingga "terenkripsi di DB" tidak berubah menjadi "terbuka untuk semua yang bisa
// memanggil API audit".
//
// Ketiadaan permission TIDAK menyembunyikan entry-nya: aktor tetap melihat siapa mengubah
// apa dan kapan — hanya nilai pengenalnya yang tertutup. Menyembunyikan entry utuh akan
// merusak fungsi audit itu sendiri.

// QueryStore adalah driven port pembacaan audit (F5), pasangan baca dari Store.
// Diimplementasi di infra/db (AuditRepo).
type QueryStore interface {
	ByEntity(ctx context.Context, entity string, entityID uuid.UUID) ([]AuditEntry, error)
	ByTenant(ctx context.Context, tenantID string) ([]AuditEntry, error)
}

// VisibleEntry adalah satu entry audit sebagaimana BOLEH dilihat aktor tertentu.
//
// PrevHash & Hash sengaja TIDAK dibawa: nilai diff di sini sudah didekripsi (atau ditutup),
// sehingga hash entry mentah tak lagi cocok dengannya. Tipe terpisah ini menutup jebakan
// "verifikasi chain atas entry hasil baca" yang akan melaporkan tamper palsu — verifikasi
// integritas (VerifyChain) selalu memakai entry mentah dari QueryStore.
type VisibleEntry struct {
	ID           uuid.UUID
	TenantID     string
	Entity       string
	EntityID     uuid.UUID
	Action       Action
	ActorID      uuid.UUID
	ActorIP      string
	Diff         []FieldDiff
	WorkflowFrom string
	WorkflowTo   string
	Timestamp    time.Time
}

// Penanda nilai yang tidak ditampilkan. Sengaja mencolok & menyebut permission yang kurang:
// pembaca audit harus tahu ada nilai di sana yang tak ia lihat, bukan mengira field-nya kosong.
const (
	HiddenSensitive  = "[tersembunyi: butuh permission audit:sensitive:baca]"
	UndecryptableRaw = "[terenkripsi: tidak dapat dibuka dengan kunci saat ini]"
)

// Reader membaca jejak audit dengan kontrol akses atas nilai sensitif.
//
// crypto boleh nil untuk deployment tanpa enkripsi field: nilai diff yang tersimpan pun
// pasti bukan ciphertext karena repository dan Reader dirakit dari composition root yang
// sama. Bila keduanya sempat menyimpang, nilai terenkripsi tetap tampil sebagai base64
// buram — tertutup, bukan bocor.
type Reader struct {
	store  QueryStore
	crypto port.CryptoPort
}

func NewReader(store QueryStore, crypto port.CryptoPort) *Reader {
	return &Reader{store: store, crypto: crypto}
}

// ByEntity mengembalikan riwayat satu entity dalam tenant aktor. Entity milik tenant lain
// tak terjangkau karena penyaringan terjadi pada tenant di AuthContext, bukan parameter.
func (r *Reader) ByEntity(actx port.AuthContext, entity string, entityID uuid.UUID) ([]VisibleEntry, error) {
	entries, err := r.store.ByEntity(actx, entity, entityID)
	if err != nil {
		return nil, err
	}
	return r.reveal(actx, r.filterTenant(actx.TenantID(), entries)), nil
}

// ByTenant mengembalikan seluruh jejak tenant aktor. tenant_id TIDAK diterima sebagai
// parameter — ia selalu dari token tersigning, sehingga aktor tak bisa membaca audit
// tenant lain hanya dengan mengganti argumen.
func (r *Reader) ByTenant(actx port.AuthContext) ([]VisibleEntry, error) {
	entries, err := r.store.ByTenant(actx, actx.TenantID())
	if err != nil {
		return nil, err
	}
	return r.reveal(actx, entries), nil
}

// filterTenant menjaga invariant "aktor hanya melihat tenantnya" meski store dipanggil
// dengan kunci yang tidak ber-tenant (ByEntity mencari lewat entity+id).
func (r *Reader) filterTenant(tenantID string, entries []AuditEntry) []AuditEntry {
	out := make([]AuditEntry, 0, len(entries))
	for _, e := range entries {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out
}

// reveal menyalin entry ke bentuk tampil, membuka nilai terenkripsi bila aktor berhak.
// Permission diperiksa SEKALI di sini, bukan per-nilai: hasilnya sama untuk seluruh
// pembacaan dan pemeriksaan per-nilai hanya menambah biaya.
func (r *Reader) reveal(actx port.AuthContext, entries []AuditEntry) []VisibleEntry {
	boleh := actx.RequirePermission(PermSensitiveBaca) == nil

	out := make([]VisibleEntry, 0, len(entries))
	for _, e := range entries {
		diff := make([]FieldDiff, len(e.Diff))
		for i, d := range e.Diff {
			diff[i] = FieldDiff{
				Field:  d.Field,
				Before: r.revealValue(actx, e.TenantID, d.Before, boleh),
				After:  r.revealValue(actx, e.TenantID, d.After, boleh),
			}
		}
		out = append(out, VisibleEntry{
			ID: e.ID, TenantID: e.TenantID, Entity: e.Entity, EntityID: e.EntityID,
			Action: e.Action, ActorID: e.ActorID, ActorIP: e.ActorIP, Diff: diff,
			WorkflowFrom: e.WorkflowFrom, WorkflowTo: e.WorkflowTo, Timestamp: e.Timestamp,
		})
	}
	return out
}

// revealValue menentukan nasib satu nilai diff.
//
// Yang menandai sebuah nilai "sensitif" adalah bentuknya sendiri — ciphertext framework yang
// dikenali PurposeOf — bukan klasifikasi field yang dibaca ulang saat baca. Ini disengaja:
// class sebuah field bisa berubah setelah entry lama tertulis, dan jejak audit harus tetap
// diperlakukan sesuai apa yang BENAR-BENAR tersimpan di dalamnya.
func (r *Reader) revealValue(ctx context.Context, tenantID string, v any, boleh bool) any {
	s, ok := v.(string)
	if !ok || s == "" || r.crypto == nil {
		return v
	}
	ct, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return v // nilai biasa (class public/internal/personal) — tampil apa adanya
	}
	if _, err := r.crypto.PurposeOf(ct); err != nil {
		return v // base64 yang kebetulan sah tapi bukan ciphertext framework
	}
	if !boleh {
		return HiddenSensitive
	}
	plain, err := r.crypto.Decrypt(ctx, tenantID, ct)
	if err != nil {
		// Kunci tenant lain / kunci hilang / blob rusak. Jangan pernah mengembalikan blob
		// mentah sebagai "nilai" — itu menyamarkan kegagalan sebagai data.
		return UndecryptableRaw
	}
	return string(plain)
}
