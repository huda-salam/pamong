package audit

import (
	"reflect"
	"sort"
)

// Diff membandingkan snapshot field before vs after dan mengembalikan hanya field
// yang berubah, terurut nama field agar deterministik (penting untuk hash chain).
//
//   - create: before nil/kosong  -> semua field after muncul sebagai perubahan (Before nil)
//   - delete: after nil/kosong    -> semua field before muncul sebagai perubahan (After nil)
//   - update: hanya selisihnya
func Diff(before, after map[string]any) []FieldDiff {
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}

	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var diffs []FieldDiff
	for _, k := range sorted {
		b, bok := before[k]
		a, aok := after[k]
		if bok && aok && reflect.DeepEqual(b, a) {
			continue // tidak berubah
		}
		// Field yang hanya ada di satu sisi tetap dicatat (mis. create/delete).
		diffs = append(diffs, FieldDiff{Field: k, Before: b, After: a})
	}
	return diffs
}
