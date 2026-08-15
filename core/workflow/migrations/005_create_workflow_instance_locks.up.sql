-- Kunci transisi instance, ber-SEWA (PR-W4a perbaikan review; menggantikan advisory lock).
--
-- Versi pertama W4a memakai `pg_try_advisory_xact_lock` di dalam transaksi yang dibuka khusus dan
-- ditahan SELAMA transisi berjalan. Itu benar secara saling-meniadakan tapi salah secara liveness:
-- action yang berjalan di bawah kunci memakai POOL YANG SAMA (satu pool per tenant DB), sehingga
-- setiap transisi menahan satu koneksi mati selama use case bekerja. Pada tenant yang sibuk, N
-- transisi bersamaan = N koneksi tertahan → seluruh pool tenant habis dan SELURUH request tenant
-- itu (bukan hanya workflow) menggantung menunggu koneksi.
--
-- Sewa memecahnya: kunci adalah BARIS, bukan sesi. Nol koneksi ditahan selama action berjalan.
-- Presedennya sudah hidup di repo ini — gov.job_locks (infra/scheduler.DBLocker) memakai bentuk
-- yang sama untuk lock scheduler lintas replika.
--
-- locked_until adalah pagar terhadap proses yang MATI di tengah transisi: tanpanya kunci baris
-- akan yatim selamanya (advisory lock tak punya masalah ini karena ikut mati bersama koneksinya).
-- Konsekuensinya diterima secara sadar: kunci yang kedaluwarsa saat action-nya masih berjalan
-- membuka celah transisi ganda, jadi TTL dipilih jauh di atas durasi request yang wajar
-- (lihat instanceLockTTL di infra/workflow/instance_store.go).
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.workflow_instance_locks (
    instance_id  UUID PRIMARY KEY,
    token        UUID        NOT NULL,  -- pemegang saat ini; release hanya oleh pemegang
    locked_until TIMESTAMPTZ NOT NULL   -- batas sewa; jam DATABASE, bukan jam aplikasi
);

-- Sapuan kunci kedaluwarsa (housekeeping opsional; jalur normal menghapus lewat primary key).
CREATE INDEX IF NOT EXISTS idx_wfinst_lock_expiry
    ON gov.workflow_instance_locks (locked_until);
