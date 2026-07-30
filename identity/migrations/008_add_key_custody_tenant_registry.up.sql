-- Custody KEK sebagai kebijakan PER-TENANT (ADR-010 §3). PR-3.8.2.
--
-- 'platform' = KEK dipegang KeyProvider yang dikelola platform (default Tier 1/2).
-- 'tenant'   = KEK dipegang KeyProvider milik pemda (mis. Vault/HSM pemda di Tier 3).
--
-- Kolom ini SENGAJA ditambahkan sekarang meski mesin custody penuh belum ada: PR-3.8.2
-- hanya mendukung mode 'platform'; tenant ber-custody 'tenant' DITOLAK LANTANG oleh
-- resolver (bukan diam-diam jatuh ke platform — itu akan memberi jaminan kedaulatan kunci
-- yang tidak benar). Mode 'tenant' diaktifkan di PR-3.8.8 dengan mendaftarkan driver
-- KeyProvider pemda — tanpa mengubah kode kripto maupun skema ini.
--
-- Nilai diubah lewat UPDATE saat onboarding/kontrak per-pemda (belum ada jalur CLI/UI di
-- PR-3.8.2 — disengaja: mengubah custody adalah keputusan kontraktual, bukan operasi rutin).
ALTER TABLE id.tenant_registry
    ADD COLUMN key_custody VARCHAR(10) NOT NULL DEFAULT 'platform'
        CHECK (key_custody IN ('platform','tenant'));
