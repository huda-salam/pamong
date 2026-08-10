-- Down WAJIB ada (linter: migration-needs-down).
--
-- Menghapus sentinel hanya berhasil selama tak ada baris yang merujuknya. Bila sudah ada
-- penugasan ber-assigned_by sentinel, FK menolak DELETE ini — dan itu perilaku yang BENAR:
-- menghapusnya diam-diam akan memutus ketelusuran baris-baris tersebut. Pulihkan dengan
-- menjalankan up kembali, atau tentukan aktor pengganti untuk baris yang merujuk lebih dulu.
DELETE FROM id.persons WHERE id = '00000000-0000-0000-0000-000000000001';
