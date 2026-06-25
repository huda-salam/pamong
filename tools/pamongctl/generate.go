package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/modules"
	"github.com/spf13/cobra"
)

// generateCmd: generate artefak dari definisi (PR-1.2.4).
func generateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "generate",
		Short: "Generate artefak dari definisi (mis. migrasi dari EntityDef)",
	}
	c.AddCommand(generateMigrationCmd())
	return c
}

func generateMigrationCmd() *cobra.Command {
	var name, modulesDir string
	c := &cobra.Command{
		Use:   "migration [modul]",
		Short: "Generate file migrasi up/down baseline dari EntityDef modul",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateMigration(cmd, args[0], name, modulesDir)
		},
	}
	c.Flags().StringVar(&name, "name", "", "nama migrasi (default: create_{modul})")
	c.Flags().StringVar(&modulesDir, "modules", "modules", "direktori akar modul")
	return c
}

func runGenerateMigration(cmd *cobra.Command, moduleName, name, modulesDir string) error {
	mod := findModule(moduleName)
	if mod == nil {
		return fmt.Errorf("modul %q tidak terdaftar di modules.All()", moduleName)
	}
	entities := mod.Manifest().Entities
	if len(entities) == 0 {
		return fmt.Errorf("modul %q tidak punya entity untuk dimigrasikan", moduleName)
	}
	schema := entities[0].Schema
	for _, e := range entities {
		if e.Schema != schema {
			return fmt.Errorf("entity %q punya schema %q, beda dari %q — satu modul satu schema",
				e.Name, e.Schema, schema)
		}
	}

	up, down, err := db.GenerateMigration(schema, entities)
	if err != nil {
		return err
	}

	if name == "" {
		name = "create_" + moduleName
	}
	dir := filepath.Join(modulesDir, moduleName, "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	version, err := nextVersion(dir)
	if err != nil {
		return err
	}

	upPath := filepath.Join(dir, fmt.Sprintf("%s_%s.up.sql", version, name))
	downPath := filepath.Join(dir, fmt.Sprintf("%s_%s.down.sql", version, name))
	if err := writeNew(upPath, up); err != nil {
		return err
	}
	if err := writeNew(downPath, down); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "dibuat %s\n", upPath)
	fmt.Fprintf(cmd.OutOrStdout(), "dibuat %s\n", downPath)
	return nil
}

func findModule(name string) domain.Module {
	for _, m := range modules.All() {
		if m.Manifest().Name == name {
			return m
		}
	}
	return nil
}

var versionPrefixRe = regexp.MustCompile(`^(\d+)_`)

// nextVersion memindai dir migrasi, mengambil prefix numerik tertinggi, lalu +1
// dengan lebar nol-pad 3 (001, 002, ...).
func nextVersion(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	max := 0
	for _, e := range entries {
		m := versionPrefixRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n > max {
			max = n
		}
	}
	return fmt.Sprintf("%03d", max+1), nil
}

// writeNew menulis file baru; menolak menimpa file yang sudah ada (migrasi append-only).
func writeNew(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file %s sudah ada — migrasi bersifat append-only, jangan timpa", path)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
