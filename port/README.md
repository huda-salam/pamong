# port/ — Kontrak lintas komponen

Semua interface (port) yang menjadi kontrak antar layer dan antar modul.
File di sini adalah "konstitusi" — perubahan butuh ADR.

Modul bisnis HANYA bergantung pada file di folder ini (+ standard library).
Tidak boleh import core/, infra/, atau modul lain secara langsung.

## Port terencana (belum diimplementasi)
- `crypto.go` — `CryptoPort` (Encrypt/Decrypt/BlindIndex) untuk enkripsi field selektif +
  blind index (ADR-009). Impl di `infra/crypto`, dipakai transparan oleh `infra/db`.
  DEFERRED pasca-Phase 3 (lihat ROADMAP sub-phase kripto).
