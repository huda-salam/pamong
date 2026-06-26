-- Rollback PR-2.3.2. Urutan terbalik dari up (hormati FK).
DROP TABLE IF EXISTS id.central_role_assignments;
DROP TABLE IF EXISTS id.central_role_permissions;
DROP TABLE IF EXISTS id.central_roles;
