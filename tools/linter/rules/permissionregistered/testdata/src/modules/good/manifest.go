package good

// Modul good meng-import satu permission milik modul "other". Di manifest.go itulah
// satu-satunya tempat literal permission lintas-modul yang sah muncul (entri Imports).
// Analyzer membaca file ini sebagai allow-list.
var _imports = []string{
	"other:thing:baca", // {From: "other", Permission: "other:thing:baca"}
}
