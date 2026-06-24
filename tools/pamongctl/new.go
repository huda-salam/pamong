package main

import "github.com/spf13/cobra"

// newCmd: scaffolding modul/entity. Implementasi penuh di PR-5.2.1.
func newCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new",
		Short: "Scaffold modul atau entity baru (struktur hexagonal)",
	}
	c.AddCommand(&cobra.Command{
		Use:   "module",
		Short: "Generate struktur modul hexagonal lengkap",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "PR-5.2.1")
		},
	})
	return c
}
