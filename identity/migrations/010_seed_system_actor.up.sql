-- Seed person SENTINEL "sistem" (PR-W2). Menutup backlog "Sentinel SYSTEM actor".
--
-- MASALAH: id.tenant_assignments.assigned_by dan id.central_role_assignments.assigned_by
-- keduanya NOT NULL ber-FK ke id.persons(id). Aturan itu benar — setiap pemberian wewenang
-- harus bisa ditelusuri ke seseorang — tapi ia membuat penugasan PERTAMA mustahil: admin
-- pertama belum punya siapa pun yang bisa menugaskannya. Melonggarkan FK (atau membolehkan
-- NULL) menghapus ketelusuran SELURUH baris demi satu baris pertama.
--
-- SOLUSI: satu baris tetap, ber-id `00000000-0000-0000-0000-000000000001` — seluruh oktet nol
-- kecuali digit terakhir, dan bukan UUIDv4 yang sah (nibble versi & varian nol), jadi ia tak
-- akan pernah bertabrakan dengan id dari uuid.New(). Padanan Go-nya: identity/domain.SystemActorID.
--
-- KENAPA nik_enc/nik_bidx KOSONG (bytea zero-length, bukan NULL): kolomnya NOT NULL dan migrasi
-- tak punya akses ke KeyProvider, jadi ia tak bisa mengenkripsi apa pun. Nilai kosong justru
-- yang diinginkan, dan sifatnya diandalkan:
--   * crypto.FieldSealer.Open memetakan kolom kosong → string kosong, jadi FindByID(sentinel)
--     mengembalikan person ber-NIK "" tanpa error dekripsi;
--   * FindByNIK("") TIDAK menemukannya — blind index dari "" adalah HMAC, bukan bytes kosong,
--     jadi sentinel tak terjangkau lewat pencarian NIK mana pun;
--   * uq_persons_nik_bidx tetap utuh: person nyata selalu punya NIK 16 digit (Person.Validate),
--     jadi tak ada baris lain yang bisa menempati bidx kosong.
--
-- is_active = false: sentinel bukan orang yang bisa masuk sistem. Ia tak punya credential, tak
-- punya employment, dan tak pernah menjadi subjek permission — ia hanya menjawab "siapa yang
-- menugaskan" saat jawabannya memang "sistem, saat bootstrap".
--
-- ON CONFLICT DO NOTHING membuat migrasi ini aman dijalankan ulang (identity DB belum punya
-- runner ber-tracking; jalur B, dijalankan ops).
INSERT INTO id.persons (id, nik_enc, nik_bidx, nama_lengkap, is_active)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    ''::bytea,
    ''::bytea,
    'SYSTEM (sentinel)',
    false
)
ON CONFLICT (id) DO NOTHING;
