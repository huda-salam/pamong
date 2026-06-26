package usecase

// permForeign permission modul "other" yang TIDAK terdaftar di Imports manifest bad.
const permForeign = "other:thing:baca" // want `permission "other:thing:baca" milik modul lain dan tidak terdaftar`

func Use() string { return permForeign }
