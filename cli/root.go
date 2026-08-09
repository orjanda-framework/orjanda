// Package cli implements the orjanda CLI commands backing cmd/orjanda
// (TAD §16). The full command surface arrives in Phase 10; Phase 8 ships the
// `agent chat` terminal-mode entry point.
package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the orjanda root command with its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "orjanda",
		Short: "Orjanda — an agent-native business application framework",
		Long:  "Orjanda — an agent-native business application framework. Full CLI lands in Phase 10 (TAD §16, PRD §21).",
	}

	root.AddCommand(newAgentChatCmd())

	return root
}
