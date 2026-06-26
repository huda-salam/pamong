package usecase

// permOwn permission milik modul good — boleh dipakai sebagai konstanta lokal.
const permOwn = "good:doc:buat"

// permImported permission modul "other" yang TERDAFTAR di Imports manifest good — boleh.
const permImported = "other:thing:baca"

// Use memakai keduanya; tak satu pun melanggar.
func Use() string { return permOwn + permImported }
