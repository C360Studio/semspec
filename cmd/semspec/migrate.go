package main

import (
	"github.com/spf13/cobra"
)

// migrateCmd returns the `semspec migrate` parent command.
// All migrations have been run; this command is retained as a placeholder.
func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Data migration utilities",
		Long:  "Run-once data migration commands for upgrading semspec data structures.",
	}
	return cmd
}
