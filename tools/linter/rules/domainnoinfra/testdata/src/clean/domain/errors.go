package domain

import "errors"

// ErrNomorKosong adalah domain error murni.
var ErrNomorKosong = errors.New("nomor surat tidak boleh kosong")
