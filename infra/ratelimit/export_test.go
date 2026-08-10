package ratelimit

// Expose internal ke test eksternal (ratelimit_test): batas & interval yang jadi kontrak
// perilaku, plus dua pengintip keadaan yang tak layak jadi API publik.

const (
	MaxEntriesForTest     = maxEntries
	RotateIntervalForTest = rotateInterval
)

// LenForTest melaporkan total jendela tersimpan (kedua generasi).
func (m *Memory) LenForTest() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cur) + len(m.prev)
}

// RotationsForTest melaporkan berapa kali generasi dirotasi — satu-satunya mekanisme pembuangan,
// dan satu-satunya operasi non-O(1) yang tersisa (melepas map).
func (m *Memory) RotationsForTest() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rotations
}
