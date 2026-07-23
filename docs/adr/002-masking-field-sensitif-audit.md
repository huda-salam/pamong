# ADR-002: Perlakuan field sensitif dalam audit trail

## Status
Accepted — keputusan inti tetap berlaku; **rencana implementasi diperbarui oleh ADR-009**
(penanda `Sensitive bool` digantikan sumbu `DataClass`; diff class `personal_id`+ ikut
terenkripsi, nilai raw tetap ada sebagai bukti). Lihat ADR-009 §"Hubungan dengan ADR-002".

## Konteks
PRD audit (F2) menyebut field sensitif (mis. NIK, NIP, nomor rekening) "di-mask bila
perlu". Pada Phase 1.3, `Diff(before, after)` mencatat **semua** field yang berubah apa
adanya; nilai mentah tersimpan di `gov.audit_logs.diff` (JSONB). Audit log dibaca cukup
luas (`platform_auditor`, helpdesk), sehingga menyimpan data pribadi mentah berpotensi
over-ekspos dan bersinggungan dengan UU PDP.

Belum ada (a) penanda field sensitif di `FieldDef`, maupun (b) logika perlakuan khusus
di audit engine. Selain itu ada interaksi dengan hash chain: bila nilai diubah/di-mask
sebelum di-hash, chain konsisten tapi nilai asli tak lagi bisa dibuktikan dari audit;
bila di-hash mentah lalu di-mask saat tampil, nilai asli tetap ada di DB.

## Keputusan
Strategi: **simpan nilai mentah + kontrol akses saat baca** (bukan masking saat tulis).
Alasan: pemeriksa (BPK) umumnya membutuhkan nilai asli before/after sebagai bukti;
masking saat simpan menghancurkan bukti secara permanen dan tak bisa dipulihkan. Privasi
ditegakkan di lapis baca lewat permission, bukan dengan merusak data di lapis tulis.

Implementasi ditunda ke **Phase 2 (identity & permission)**, karena: field sensitif
(NIK/NIP) pertama kali muncul nyata di sana, dan kontrol akses membutuhkan permission
engine yang baru dibangun di sub-phase 2.3.

Rencana implementasi (di Phase 2):
- Tambah penanda `Sensitive bool` pada `FieldDef` (deklaratif di EntityDef).
- Query/replay audit menyaring/menyamarkan field `Sensitive` bagi pembaca tanpa
  permission khusus (mis. `audit:sensitive:baca`). Nilai mentah tetap utuh di DB dan
  ikut hash chain.

## Konsekuensi
- Sampai Phase 2, nilai sensitif tersimpan mentah dan terlihat oleh siapa pun yang bisa
  membaca audit. Diterima sementara karena Phase 1 belum punya entity ber-NIK nyata
  (identity ada di Phase 2) dan belum ada permission engine untuk menegakkan filter.
- Hash chain selalu dihitung atas nilai mentah → bukti integritas tetap penuh.
- Keputusan "mask saat tampil, bukan saat simpan" mengikat desain query audit nanti.

## Alternatif yang dipertimbangkan
- **Mask penuh saat simpan** ("field berubah" tanpa nilai). Privasi maksimal, tapi
  menghapus bukti before/after permanen — bertentangan dengan kebutuhan forensik BPK.
  Ditolak.
- **Mask parsial saat simpan** (mis. 4 digit terakhir). Masih membocorkan sebagian,
  butuh aturan per-tipe, dan tetap merusak bukti. Ditolak.
