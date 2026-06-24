# port/ — Kontrak lintas komponen

Semua interface (port) yang menjadi kontrak antar layer dan antar modul.
File di sini adalah "konstitusi" — perubahan butuh ADR.

Modul bisnis HANYA bergantung pada file di folder ini (+ standard library).
Tidak boleh import core/, infra/, atau modul lain secara langsung.
