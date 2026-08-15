package cli

import (
	"fmt"

	"github.com/orjanda-framework/orjanda/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the CLI/framework version",
		Long:  "Print the CLI/framework version detected from build metadata (TAD §18).",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Current()
			if verbose {
				printVerboseVersion(info)
			} else {
				printVersion(info)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print detailed version information")
	return cmd
}

func printVersion(info version.Info) {
	fmt.Printf("orjanda %s\n", info.Version)
}

func printVerboseVersion(info version.Info) {
	fmt.Printf("orjanda %s\n", info.Version)
	fmt.Printf("  ModulePath:  %s\n", info.ModulePath)
	fmt.Printf("  GoVersion:   %s\n", info.GoVersion)
	if info.VCSRevision != "" {
		fmt.Printf("  VCSRevision: %s\n", info.VCSRevision)
	}
	if info.VCSDirty {
		fmt.Printf("  VCSDirty:    true\n")
	}
}
