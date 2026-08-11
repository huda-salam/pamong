package permission

import (
	"github.com/google/uuid"

	"github.com/huda-salam/pamong/port"
)

// RequireAuthorityOver menegakkan bahwa actor berwenang atas JANGKAUAN yang ia berikan — bukan
// hanya memegang permission-nya (ADR-021). Dipakai use case yang MEMBERIKAN wewenang ber-scope
// unit: penugasan role tenant dan pembuatan delegasi.
//
// Dua kasus, dan yang kedua yang mudah terlewat:
//
//   - unit disebut TANPA subtree → `RequirePermissionInUnit(perm, *unit)`: jangkauan yang diberikan
//     harus berada dalam jangkauan actor (unitnya sendiri, keturunannya bila subtree, atau
//     se-tenant).
//   - unit disebut DENGAN subtree → `RequirePermissionInSubtree(perm, *unit)`. Memberi
//     `include_subtree` atas sebuah unit berarti memberi jangkauan atas SELURUH KETURUNANNYA,
//     jadi ia menuntut wewenang yang menjangkau keturunan itu — bukan wewenang atas unit itu saja.
//     Tanpa cabang ini, pemegang wewenang atas satu unit saja bisa menerbitkan assignment
//     ber-`include_subtree` pada unitnya dan dengan begitu membagikan jangkauan atas seluruh
//     turunan yang ia sendiri tak punya. Bentuk eskalasi yang sama diam-diamnya dengan
//     mengosongkan `unit_kerja_id`, hanya lewat boolean.
//   - unit nil (= SELURUH TENANT) → `RequirePermissionInUnit(perm, uuid.Nil)`. Ini bukan trik:
//     "se-tenant" adalah jangkauan TERLUAS, jadi ia menuntut wewenang terluas. uuid.Nil tak pernah
//     menjadi unit nyata — `Validate` pada kedua domain (tenantrole & delegation) menolak unit
//     ber-UUID nol dan mewajibkan nil untuk "seluruh tenant" — sehingga satu-satunya grant yang
//     bisa menutupinya adalah grant TenantWide. Tanpa cabang ini, admin ber-scope satu OPD cukup
//     MENGOSONGKAN `unit_kerja_id` untuk menugaskan se-tenant: eskalasi lewat field yang dibiarkan
//     kosong, bentuk yang paling sering lolos review.
//
// Lapis RBAC tidak dipanggil terpisah: Tahap 1 `AllowsInUnit` sudah `Engine.Allows` (RBAC utuh
// dengan strict-intersection & global-precedence), jadi memanggil `RequirePermission` lebih dulu
// hanya menduplikasi keputusan yang sama — dan duplikasi itulah yang kelak menyimpang.
//
// Ia tinggal di core/permission, bukan disalin ke tiap paket use case, karena ia ATURAN
// otorisasi: dua salinan akan menyimpang saat salah satunya diperbaiki (alasan yang sama dengan
// `crypto.FieldSealer` dan `passwordAuthenticator`).
//
// CATATAN PENTING soal default permisif: `RequirePermissionInUnit` permisif bila gateway.Context
// tak punya ScopedEvaluator. Sejak PR-W3b middleware SELALU memasangnya untuk request ber-token
// (Authority KOSONG untuk konteks tanpa tenant), jadi jalur produksi tak lagi punya lubang itu.
// Pemanggil di luar jalur HTTP (CLI, importer, job) WAJIB menyediakan konteks ber-evaluator —
// kalau tidak, aturan ini diam-diam tak menegakkan apa pun.
// `includeSubtree` diabaikan saat unit nil: "seluruh tenant" sudah mencakup setiap keturunan, jadi
// tak ada jangkauan tambahan yang bisa diberikan boolean itu.
func RequireAuthorityOver(ctx port.AuthContext, perm string, unit *uuid.UUID, includeSubtree bool) error {
	if unit == nil {
		return ctx.RequirePermissionInUnit(perm, uuid.Nil)
	}
	if includeSubtree {
		return ctx.RequirePermissionInSubtree(perm, *unit)
	}
	return ctx.RequirePermissionInUnit(perm, *unit)
}
