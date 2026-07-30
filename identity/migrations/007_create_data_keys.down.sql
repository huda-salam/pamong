-- Turun: hapus store DEK ter-wrap.
-- PERINGATAN: menjalankan ini pada DB yang sudah memuat data terenkripsi membuat seluruh
-- kolom _enc TIDAK BISA didekripsi lagi (DEK hilang). Hanya untuk lingkungan dev/test.
DROP INDEX IF EXISTS id.uq_data_keys_active;
DROP TABLE IF EXISTS id.data_keys;
