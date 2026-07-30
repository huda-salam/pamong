-- Turun: kembalikan registry tanpa kebijakan custody (semua tenant implisit 'platform').
ALTER TABLE id.tenant_registry DROP COLUMN IF EXISTS key_custody;
