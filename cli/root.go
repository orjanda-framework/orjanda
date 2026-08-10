// Package cli implements the orjanda CLI commands backing cmd/orjanda
// (TAD §16 / PRD §21). The same command implementations power Application
// binaries via cli.Main (see main.go).
package cli

import (
	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda"
)

// NewRootCmd builds the orjanda root command with its subcommands.
func NewRootCmd(configure func(*orjanda.Site) error) *cobra.Command {
	root := &cobra.Command{
		Use:   "orjanda",
		Short: "Orjanda — an agent-native business application framework",
		Long:  "Orjanda — an agent-native business application framework (TAD §16, PRD §21).",
	}

	b := newSiteBuilder(configure)

	root.AddCommand(
		newInitCmd(),
		newNewCmd(),
		newServeCmd(b),
		newBenchCmd(b),
		newMigrateCmd(b),
		newConsoleCmd(b),
		newInstallCmd(b),
		newUninstallCmd(b),
		newTestCmd(),
		newAgentCmd(b),
		newRegistryCmd(b),
	)

	return root
}
