-- Down WAJIB ada (linter: migration-needs-down). Urutan terbalik dari up.
DROP TABLE IF EXISTS surat_masuk.disposisis;
DROP TABLE IF EXISTS surat_masuk.surat_masuks;
DROP SCHEMA IF EXISTS surat_masuk;
