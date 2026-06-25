package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SeedHash adalah PrevHash untuk entry pertama dalam sebuah chain (per tenant).
// Konstan & publik agar verifikasi independen bisa mereproduksi chain dari awal.
const SeedHash = "PAMONG-AUDIT-GENESIS"

// ComputeHash menghitung hash kanonik sebuah entry: H(prev_hash + konten kanonik).
// Konten diserialisasi deterministik (urutan field tetap, diff sudah terurut oleh
// Diff(), timestamp dalam UTC presisi mikrodetik agar konsisten dengan penyimpanan
// Postgres TIMESTAMPTZ). Hash inilah yang merantai entry dan mendeteksi manipulasi.
func ComputeHash(prevHash string, e AuditEntry) string {
	diffJSON, _ := json.Marshal(e.Diff)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
		prevHash,
		e.ID,
		e.TenantID,
		e.Entity,
		e.EntityID,
		e.Action,
		e.ActorID,
		e.ActorIP,
		diffJSON,
		e.WorkflowFrom,
		e.WorkflowTo,
		e.Timestamp.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// VerifyResult melaporkan hasil verifikasi sebuah chain.
type VerifyResult struct {
	OK       bool
	BrokenAt int       // indeks entry yang putus (0-based); -1 bila utuh
	EntryID  uuid.UUID // entry tempat chain putus
	Reason   string
}

// VerifyChain menelusuri entries (terurut kronologis per tenant), menghitung ulang
// hash, dan mendeteksi titik pertama chain putus: konten dimodifikasi (hash != konten),
// atau entry dihapus/disisipkan (prev_hash tidak menyambung).
func VerifyChain(entries []AuditEntry) VerifyResult {
	prev := SeedHash
	for i, e := range entries {
		if e.PrevHash != prev {
			return VerifyResult{
				OK: false, BrokenAt: i, EntryID: e.ID,
				Reason: "prev_hash tidak menyambung — entry sebelumnya dihapus atau ada penyisipan",
			}
		}
		if want := ComputeHash(prev, e); want != e.Hash {
			return VerifyResult{
				OK: false, BrokenAt: i, EntryID: e.ID,
				Reason: "hash tidak cocok dengan konten — entry telah dimodifikasi",
			}
		}
		prev = e.Hash
	}
	return VerifyResult{OK: true, BrokenAt: -1}
}
