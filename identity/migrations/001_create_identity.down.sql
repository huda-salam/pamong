-- Down WAJIB ada (linter: migration-needs-down). Urutan terbalik dari up.
DROP TABLE IF EXISTS id.credentials;
DROP TABLE IF EXISTS id.employments;
DROP TABLE IF EXISTS id.persons;
DROP SCHEMA IF EXISTS id;
