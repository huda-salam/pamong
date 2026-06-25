package audit_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
)

// chained membangun chain valid dari beberapa entry (mensimulasikan apa yang dilakukan
// store: prev_hash = hash entry sebelumnya, hash = ComputeHash(prev, entry)).
func chained(n int) []audit.AuditEntry {
	out := make([]audit.AuditEntry, n)
	prev := audit.SeedHash
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		e := audit.AuditEntry{
			ID: uuid.New(), TenantID: "t1", Entity: "m.E", EntityID: uuid.New(),
			Action: audit.ActionUpdate, ActorID: uuid.New(),
			Diff:      []audit.FieldDiff{{Field: "x", Before: i, After: i + 1}},
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			PrevHash:  prev,
		}
		e.Hash = audit.ComputeHash(prev, e)
		out[i] = e
		prev = e.Hash
	}
	return out
}

func TestVerifyChain_Intact(t *testing.T) {
	res := audit.VerifyChain(chained(4))
	if !res.OK || res.BrokenAt != -1 {
		t.Fatalf("chain valid harus OK, dapat %+v", res)
	}
}

func TestVerifyChain_Empty(t *testing.T) {
	if res := audit.VerifyChain(nil); !res.OK {
		t.Fatalf("chain kosong harus OK, dapat %+v", res)
	}
}

func TestVerifyChain_DetectsModifiedContent(t *testing.T) {
	entries := chained(4)
	// Ubah konten entry #2 tanpa memperbarui hash → hash tak lagi cocok dengan konten.
	entries[2].Diff = []audit.FieldDiff{{Field: "x", Before: 99, After: 100}}

	res := audit.VerifyChain(entries)
	if res.OK || res.BrokenAt != 2 {
		t.Fatalf("modifikasi entry #2 harus terdeteksi, dapat %+v", res)
	}
}

func TestVerifyChain_DetectsDeletedEntry(t *testing.T) {
	entries := chained(4)
	// Hapus entry #1: entry #2 (kini di indeks 1) punya prev_hash entry lama → putus.
	tampered := append(entries[:1], entries[2:]...)

	res := audit.VerifyChain(tampered)
	if res.OK || res.BrokenAt != 1 {
		t.Fatalf("penghapusan entry harus terdeteksi di indeks 1, dapat %+v", res)
	}
}

func TestComputeHash_StableAcrossEqualEntries(t *testing.T) {
	e := chained(1)[0]
	if audit.ComputeHash(e.PrevHash, e) != e.Hash {
		t.Fatal("ComputeHash harus deterministik untuk entry yang sama")
	}
}
