// Package db adalah stub lapisan infrastruktur untuk keperluan testdata.
// Keberadaannya membuat import "govframework/infra/db" dari domain bisa di-resolve,
// sehingga analyzer benar-benar menguji jalur "import ke lapisan infra".
package db

// Conn adalah simbol dummy yang diimport oleh testdata domain kotor.
var Conn struct{}
