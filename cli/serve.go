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
		Short: "Start the Orjanda site (development or production per ORJANDA_ENV)",
		Long:  "Starts the HTTP server for the environment selected by ORJANDA_ENV (or the env config key). development (default) auto-creates missing tables, generates an ephemeral JWT secret when none is configured, and warns-and-continues on Registry errors; production fails fast on any Registry, migration, or committed-codegen error and requires a real auth.jwt_secret (TAD §16).",
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

// forwardConfig returns the --config args to forward when delegating.
func forwardConfig(cfgFile string) []string {
	if cfgFile == "" {
		return nil
	}
	return []string{"--config", cfgFile}
}

func runServe(ctx context.Context, b siteBuilder, cfgFile string, port int) error {
	cfg, generated, err := config.Load(cfgFile)
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

	switch cfg.Env {
	case config.EnvDevelopment:
		return serveDevelopment(ctx, site, *cfg, generated)
	case config.EnvProduction:
		return serveProduction(ctx, site, *cfg)
	default:
		// Unreachable: config.Load validates ORJANDA_ENV.
		return errf("serve: unsupported ORJANDA_ENV %q", cfg.Env)
	}
}

// serveDevelopment is the forgiving environment (TAD §16): warn-and-continue
// on Registry errors, auto-create missing tables, bootstrap the first admin,
// and regenerate the TypeScript client.
func serveDevelopment(ctx context.Context, site *orjanda.Site, cfg config.Config, generated string) error {
	if generated != "" {
		slog.Warn("serve: auth.jwt_secret not configured; generated an ephemeral dev secret — sessions will not survive restart (set auth.jwt_secret in orjanda.yaml or ORJANDA_AUTH_JWT_SECRET for persistent sessions)")
	}
	if err := site.Compile(); err != nil {
		// development is forgiving (TAD §16): warn and continue serving
		// whatever compiled, rather than refusing to start.
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
				slog.Info("bootstrapped system administrator", "email", core.AdminEmail)
				// Print the one-time credential to stdout, never to the
				// structured log stream (REVIEW-2026-08-12 finding 12). TAD §4.2
				// step 3 requires the password reach the operator on first run.
				fmt.Printf("admin password: %s\n", password)
			}
		}
	}

	slog.Info("orjanda serve", "env", cfg.Env, "addr", serveAddr(cfg), "docs", len(site.Registry.List()))
	return server.Run(ctx, site)
}

// serveProduction is the fail-fast environment (TAD §16): any Registry error,
// pending schema change, or stale committed frontend codegen aborts startup.
// Tables are never auto-created and no admin is bootstrapped — production must
// go through `orjanda migrate up` first. This preserves every safety guarantee
// of the former `orjanda bench` command.
func serveProduction(ctx context.Context, site *orjanda.Site, cfg config.Config) error {
	if err := site.Compile(); err != nil {
		// production is fail-fast (TAD §16): no warn-and-continue.
		return err
	}

	if hasFrontend() {
		if err := productionCodegen(site, ui.RegenerateOptions{}); err != nil {
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
			if n := diff.ChangeCount(); n > 0 {
				return errf("serve: database has %d pending schema change(s) — run `orjanda migrate diff`/`migrate up` before starting", n)
			}
		}
		if tc, ok := site.DB.(tableCreater); ok {
			tc.RegisterDocs(site.Registry.List())
		}
	}

	slog.Info("orjanda serve", "env", cfg.Env, "addr", serveAddr(cfg), "docs", len(site.Registry.List()))
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
// in the working directory. development is forgiving (TAD §16): failures warn
// and continue, and an unchanged Registry (content-hash match, TAD §6.3 step 3)
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

// productionCodegen is production serve's fail-fast codegen gate (TAD §16):
// the committed TAD §6.3 step-1 payload must match the compiled Registry or
// the deployed TypeScript client is stale. It is node-free — generated output
// is a build-time artifact embedded via embed.FS (PRD §17.4), so a mismatch is
// a release-blocking error, not a re-generation trigger.
func productionCodegen(site *orjanda.Site, opts ui.RegenerateOptions) error {
	return ui.VerifyCommittedSchema(site.Registry, opts.InputPath)
}

// hasFrontend reports whether a frontend (orjanda-ui) project exists in the
// working directory; headless Applications have no generated client to keep in
// sync.
func hasFrontend() bool {
	_, err := os.Stat("orjanda-ui")
	return err == nil
}
