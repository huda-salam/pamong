package domain

// Persona adalah KONTEKS akses pada satu sesi — bukan tipe orang (CLAUDE.md "Identity").
// Ditentukan oleh portal/jalur masuk saat login, bukan oleh atribut person:
//   - employee → tersedia bila person punya employment aktif + tenant assignment (portal internal).
//   - citizen  → selalu tersedia untuk SIAPA PUN (portal publik), termasuk ASN yang mengakses
//     sebagai warga. Token citizen TIDAK membawa role internal (cegah kebocoran wewenang).
//
// Nilai ini diisi ke port.Claims.Persona saat penerbitan token dan dibaca gateway.Context.
const (
	PersonaEmployee = "employee"
	PersonaCitizen  = "citizen"
)
