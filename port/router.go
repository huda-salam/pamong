package port

import "net/http"

// Router adalah port pendaftaran rute HTTP modul.
// Driving adapter (gateway) mengimplementasi interface ini.
type Router interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
}
