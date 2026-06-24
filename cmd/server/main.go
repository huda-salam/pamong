// Package main adalah entry point binary server Pamong.
// Satu-satunya tempat modul bisnis "dipasang" ke framework — lihat CLAUDE.md #10.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/observability"
	surat_masuk "github.com/huda-salam/pamong/modules/surat_masuk"
	"github.com/huda-salam/pamong/port"
)

func main() {
	ctx := context.Background()

	// Muat config berlapis (env > local > {env} > default). Config tak valid →
	// gagal cepat saat boot, bukan error misterius saat melayani request (philosophy #4).
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "konfigurasi tidak valid:", err)
		os.Exit(1)
	}

	// Logger terstruktur dari config — JSON di produksi, level dari config.
	logger := observability.NewLogger(observability.LogOptions{
		Level:  cfg.Observ.LogLevel,
		Format: cfg.Observ.LogFormat,
	})

	// App dengan implementasi stub; driven adapter nyata di-wire pada Phase 1–3.
	app := domain.NewApp(
		nil, // DB — Phase 1.2.1
		nil, // Publisher — Phase 3.1.1
		nil, // Subscriber — Phase 3.1.1
		nil, // Sequence — Phase 1
		nil, // Metrics — Phase 3.7.2
		nil, // Storage — Phase 3.7.1
		nil, // UserResolver — Phase 2.1
		nil, // WorkflowRegistry — Phase 3.2
		nil, // Router — Phase 5.1.1
	)

	logger.Info(ctx, "memulai pamong", port.F("env", cfg.Env), port.F("tenant", cfg.TenantID))
	_ = app // App container siap; driven adapter di-wire pada Phase 1–3.

	modules := []domain.Module{
		&surat_masuk.Module{},
		// Daftarkan modul lain di sini saat dibuat.
	}

	// Tahap ini hanya membaca & mencatat manifest (registrasi). Bootstrap() penuh
	// memerlukan adapter Router (Phase 5.1.1) dan Workflow (Phase 3.2) yang belum ada,
	// jadi sengaja ditunda — bukan dipanggil dengan adapter nil.
	for _, m := range modules {
		manifest := m.Manifest()
		logger.Info(ctx, "modul terdaftar",
			port.F("module", manifest.Name),
			port.F("version", manifest.Version),
			port.F("entities", len(manifest.Entities)))
	}

	logger.Info(ctx, "pamong siap — bootstrap modul & HTTP server menyusul (Phase 3/5)")
}
