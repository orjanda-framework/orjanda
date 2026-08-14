package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/dal"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/server"
	"github.com/orjanda-framework/orjanda/ui"
)

func newServeCmd(b siteBuilder) *cobra.Command {
	var cfgFile string
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the development server",
		Long:  "Dev server: auto-creates missing tables and warns-and-continues on Registry errors (TAD §16).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := forwardConfig(cfgFile)
			if port > 0 {
				args = append(args, "--port", strconv.Itoa(port))
			}
			if delegated, err := b.delegateToApp(ctx, "serve", args...); delegated || err != nil {
				return err
			}
			return runServe(ctx, b, cfgFile, port)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().IntVar(&port, "port", 0, "HTTP port override (default: from config, 8080)")

	return cmd
}

func newBenchCmd(b siteBuilder) *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Start the Orjanda site (production)",
		Long:  "Production entrypoint: never auto-creates tables, requires pre-applied migrations, and fails fast on any Registry or migration-drift error (TAD §16).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if delegated, err := b.delegateToApp(ctx, "bench", forwardConfig(cfgFile)...); delegated || err != nil {
				return err
			}
			return runBench(ctx, b, cfgFile)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")

	return cmd
}

// forwardConfig returns the --config args to forward when delegating.
func forwardConfig(cfgFile string) []string {
	if cfgFile == "" {
		return nil
	}
	return []string{"--config", cfgFile}
}

func runServe(ctx context.Context, b siteBuilder, cfgFile string, port int) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	if port > 0 {
		cfg.Server.Port = port
	}

	site, err := b.newSite(*cfg)
	if err != nil {
		return err
	}

	if err := site.Compile(); err != nil {
		// serve is forgiving (TAD §16): warn and continue serving whatever
		// compiled, rather than refusing to start.
		slog.Warn("serve: registry compile error; starting server anyway", "error", err)
	} else {
		serveCodegenPass(ctx, site)
		if site.DB != nil {
			// Dev-only auto-create missing tables, then wire docType→table mappings.
			if tc, ok := site.DB.(tableCreater); ok {
				if err := tc.CreateTables(site.Registry.List()); err != nil {
					return err
				}
				tc.RegisterDocs(site.Registry.List())
			}
			// First-run admin bootstrap (TAD §4.2).
			if password, berr := core.Bootstrap(ctx, site.DB, site.Registry); berr != nil {
				slog.Warn("serve: bootstrap skipped", "error", berr)
			} else if password != "" {
				slog.Info("bootstrapped system administrator", "email", core.AdminEmail, "password", password)
			}
		}
	}

	slog.Info("orjanda serve", "addr", serveAddr(*cfg), "docs", len(site.Registry.List()))
	return server.Run(ctx, site)
}

func runBench(ctx context.Context, b siteBuilder, cfgFile string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	site, err := b.newSite(*cfg)
	if err != nil {
		return err
	}
	if err := site.Compile(); err != nil {
		// bench is fail-fast (TAD §16): no warn-and-continue.
		return err
	}

	if hasFrontend() {
		if err := benchCodegen(site, ui.RegenerateOptions{}); err != nil {
			return err
		}
	}

	if site.DB != nil {
		// Refuse to start against a database with pending migrations.
		if ms, ok := site.DB.(migratorSource); ok {
			mig := dal.NewMigrator(ms.Dialect(), ms.Underlying())
			diff, err := mig.Diff(ctx, site.Registry)
			if err != nil {
				return err
			}
			if n := len(diff.CreateTables) + len(diff.AlterTables); n > 0 {
				return errf("bench: database has %d pending schema change(s) — run `orjanda migrate diff`/`migrate up` before starting", n)
			}
		}
		if tc, ok := site.DB.(tableCreater); ok {
			tc.RegisterDocs(site.Registry.List())
		}
	}

	slog.Info("orjanda bench", "addr", serveAddr(*cfg), "docs", len(site.Registry.List()))
	return server.Run(ctx, site)
}

func serveAddr(cfg config.Config) string {
	host := cfg.Server.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// serveCodegenPass runs the TAD §6.3 codegen pass when a frontend tree exists
// in the working directory. serve is forgiving (TAD §16): failures warn and
// continue, and an unchanged Registry (content-hash match, TAD §6.3 step 3)
// skips the node invocation entirely.
func serveCodegenPass(ctx context.Context, site *orjanda.Site) {
	if !hasFrontend() {
		return
	}
	if ran, err := serveCodegen(ctx, site, ui.RegenerateOptions{}); err != nil {
		slog.Warn("serve: codegen skipped", "error", err)
	} else if ran {
		slog.Info("serve: regenerated TypeScript client", "docs", len(site.Registry.List()))
	}
}

// serveCodegen invokes ui.Regenerate with the site's compiled Registry. opts
// overrides the output paths / script / node binary; tests use it to point the
// pass at a scratch directory.
func serveCodegen(ctx context.Context, site *orjanda.Site, opts ui.RegenerateOptions) (bool, error) {
	opts.Registry = site.Registry
	return ui.Regenerate(ctx, opts)
}

// benchCodegen is bench's fail-fast codegen gate (TAD §16): the committed
// TAD §6.3 step-1 payload must match the compiled Registry or the deployed
// TypeScript client is stale. It is node-free — generated output is a
// build-time artifact embedded via embed.FS (PRD §17.4), so a mismatch is a
// release-blocking error, not a re-generation trigger.
func benchCodegen(site *orjanda.Site, opts ui.RegenerateOptions) error {
	return ui.VerifyCommittedSchema(site.Registry, opts.InputPath)
}

// hasFrontend reports whether a frontend (orjanda-ui) project exists in the
// working directory; headless Applications have no generated client to keep in
// sync.
func hasFrontend() bool {
	_, err := os.Stat("orjanda-ui")
	return err == nil
}
