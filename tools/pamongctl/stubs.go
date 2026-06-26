package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplemented adalah respons baku untuk perintah yang kerangkanya sudah ada tapi
// implementasinya dijadwalkan di PR berikutnya. Eksplisit menyebut PR agar tidak
// terlihat seperti bug — sesuai konvensi TODO ber-referensi PR (CODE_CONVENTION #9).
func notImplemented(cmd *cobra.Command, pr string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "perintah %q belum diimplementasi (dijadwalkan %s)\n", cmd.CommandPath(), pr)
	return nil
}

// migrateCmd: jalankan migrasi per-tenant. PR-1.2.3.
