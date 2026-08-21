package surat_masuk

import "embed"

// notificationsFS memuat template notifikasi baseline modul ke dalam BINARY, dengan alasan yang
// sama persis seperti workflowsFS: binary yang ter-deploy tak punya direktori repo, dan seed
// yang bergantung pada direktori kerja proses akan lulus setiap test lalu gagal di produksi.
//
//go:embed notifications/*.yaml
var notificationsFS embed.FS
