package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/app"
)

func newInstallCmd(b siteBuilder) *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "install <app>",
		Short: "Install an application",
		Long:  "Runs the app.Definition's OnInstall lifecycle hook against the site (TAD §7 / PRD §11.3).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cliArgs := append([]string{args[0]}, forwardConfig(cfgFile)...)
			if delegated, err := b.delegateToApp(ctx, "install", cliArgs...); delegated || err != nil {
				return err
			}
			return runInstall(ctx, b, cfgFile, args[0])
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")

	return cmd
}

func newUninstallCmd(b siteBuilder) *cobra.Command {
	var cfgFile string
	var dropTables bool

	cmd := &cobra.Command{
		Use:   "uninstall <app>",
		Short: "Uninstall an application",
		Long:  "Runs the app.Definition's OnUninstall lifecycle hook; --drop-tables tears down the app's tables (TAD §7 / PRD §11.3).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cliArgs := []string{args[0], "--config", cfgFile, "--drop-tables=" + strconv.FormatBool(dropTables)}
			if delegated, err := b.delegateToApp(ctx, "uninstall", cliArgs...); delegated || err != nil {
				return err
			}
			return runUninstall(ctx, b, cfgFile, args[0], dropTables)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().BoolVar(&dropTables, "drop-tables", false, "drop the app's tables on uninstall")

	return cmd
}

func runInstall(ctx context.Context, b siteBuilder, cfgFile, appName string) error {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return err
	}
	def, ok := findApp(site, appName)
	if !ok {
		return errf("app %q is not installed in this site", appName)
	}
	if inst, ok := any(def).(app.Installable); ok {
		return inst.OnInstall(ctx, site)
	}
	fmt.Printf("app %q declares no OnInstall hook; nothing to do (its Documents are registered at startup)\n", appName)
	return nil
}

func runUninstall(ctx context.Context, b siteBuilder, cfgFile, appName string, dropTables bool) error {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return err
	}
	def, ok := findApp(site, appName)
	if !ok {
		return errf("app %q is not installed in this site", appName)
	}
	if un, ok := any(def).(app.Uninstallable); ok {
		return un.OnUninstall(ctx, site, dropTables)
	}
	fmt.Printf("app %q declares no OnUninstall hook; nothing to do\n", appName)
	return nil
}

func findApp(site *orjanda.Site, appName string) (app.Definition, bool) {
	for _, a := range site.InstalledApps() {
		if a.Name == appName {
			return a, true
		}
	}
	return app.Definition{}, false
}
