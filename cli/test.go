package cli

import (
	"context"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	var run string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run application tests",
		Long:  "Runs `go test ./...`, routing orjanda/testing.NewTestSite to an ephemeral SQLite DB (TAD §16/§17).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTest(cmd.Context(), run)
		},
	}

	cmd.Flags().StringVar(&run, "run", "", "regexp passed through to `go test -run`")

	return cmd
}

func runTest(ctx context.Context, runFilter string) error {
	args := []string{"test", "./..."}
	if runFilter != "" {
		args = append(args, "-run", runFilter)
	}
	c := exec.CommandContext(ctx, "go", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
