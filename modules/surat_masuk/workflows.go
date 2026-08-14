package surat_masuk

import "embed"

// workflowsFS memuat definisi workflow baseline modul ke dalam BINARY. Seed dibaca dari sini,
// bukan dari disk: binary yang ter-deploy tak punya direktori repo, dan seed yang bergantung
// pada direktori kerja proses akan lulus setiap test lalu gagal di produksi.
//
//go:embed workflows/*.yaml
var workflowsFS embed.FS
