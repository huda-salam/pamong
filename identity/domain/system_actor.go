package domain

import "github.com/google/uuid"

// SystemActorID adalah person SENTINEL yang mewakili "sistem" sebagai aktor mutasi identity.
//
// Kenapa ia harus ada sebagai BARIS NYATA di id.persons: `id.tenant_assignments.assigned_by`
// dan `id.central_role_assignments.assigned_by` keduanya NOT NULL ber-FK ke id.persons(id).
// Aturan itu benar — setiap pemberian wewenang harus bisa ditelusuri ke seseorang — tapi ia
// membuat penugasan PERTAMA mustahil: admin pertama belum punya siapa pun yang bisa
// menugaskannya (chicken-and-egg). Melonggarkan FK atau membolehkan NULL akan menghapus
// ketelusuran untuk SELURUH baris demi satu baris pertama; sentinel membayar harganya sekali,
// di satu baris yang namanya sendiri mengumumkan bahwa itu bukan manusia.
//
// Nilainya TETAP dan sengaja tak menyerupai UUID acak: seluruh oktet nol kecuali digit
// terakhir. Ia juga bukan UUIDv4 yang sah (nibble versi & varian-nya nol), jadi ia tak akan
// pernah bertabrakan dengan id yang lahir dari uuid.New().
//
// Barisnya diseed migrasi `010_seed_system_actor` — bukan oleh kode aplikasi. Migrasi tak bisa
// mengenkripsi, jadi baris itu punya `nik_enc`/`nik_bidx` KOSONG (bytea zero-length, bukan
// NULL). Konsekuensi yang disengaja dan diandalkan:
//
//   - `FieldSealer.Open` memetakan kolom kosong → string kosong, jadi FindByID(SystemActorID)
//     mengembalikan person ber-NIK "" tanpa error dekripsi.
//   - `FindByNIK("")` TIDAK menemukannya: blind index dari "" adalah HMAC (bukan bytes kosong),
//     jadi sentinel tak terjangkau lewat jalur pencarian NIK mana pun.
//   - `Person.Validate` menolak NIK kosong, jadi sentinel juga tak bisa dibuat ulang, ditimpa,
//     atau ditiru lewat PersonRepo.Save — satu-satunya penulisnya adalah migrasi.
//
// Sentinel adalah AKTOR, bukan kredensial: ia tak punya credential, tak bisa login, dan tak
// pernah menjadi subjek permission. Ia hanya menjawab "siapa yang menugaskan" saat jawabannya
// memang "sistem, saat bootstrap".
var SystemActorID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// SystemActorNama adalah nama_lengkap baris sentinel. Nilainya digandakan di migrasi 010
// (SQL tak bisa membaca konstanta Go); test system_actor_test.go mengunci keduanya tetap sama.
const SystemActorNama = "SYSTEM (sentinel)"
