package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/dal"
)

func newMigrateCmd(b siteBuilder) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Schema migration management",
		Long:  "Diff, write, and apply Goose migrations via the dal.Migrator (TAD §14).",
	}
	cmd.AddCommand(
		newMigrateDiffCmd(b),
		newMigrateUpCmd(b),
		newMigrateStatusCmd(b),
	)
	return cmd
}

func newMigrateDiffCmd(b siteBuilder) *cobra.Command {
	var cfgFile, dir, dialect string
	var allowDestructive bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Generate a migration from schema changes",
		Long:  "Compares the compiled Registry against the live database and writes a versioned Goose migration (TAD §14.1). Destructive changes require --allow-destructive.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := []string{
				"diff",
				"--config", cfgFile,
				"--dir", dir,
				"--dialect", dialect,
				"--allow-destructive=" + strconv.FormatBool(allowDestructive),
			}
			if delegated, err := b.delegateToApp(ctx, "migrate", args...); delegated || err != nil {
				return err
			}
			return runMigrateDiff(ctx, b, cfgFile, dir, dialect, allowDestructive)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().StringVar(&dir, "dir", "migrations", "directory to write the migration file into")
	cmd.Flags().StringVar(&dialect, "dialect", "", "target SQL dialect (must match database.driver)")
	cmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "include destructive changes (dropped columns) in the written migration")

	return cmd
}

func newMigrateUpCmd(b siteBuilder) *cobra.Command {
	var cfgFile, dir string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply pending migrations",
		Long:  "Applies pending Goose migrations matching the active dialect (TAD §14.1 steps 4–5).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := []string{"up", "--config", cfgFile, "--dir", dir}
			if delegated, err := b.delegateToApp(ctx, "migrate", args...); delegated || err != nil {
				return err
			}
			return runMigrateUp(ctx, b, cfgFile, dir)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().StringVar(&dir, "dir", "migrations", "directory holding the migration files")

	return cmd
}

func newMigrateStatusCmd(b siteBuilder) *cobra.Command {
	var cfgFile, dir string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := []string{"status", "--config", cfgFile, "--dir", dir}
			if delegated, err := b.delegateToApp(ctx, "migrate", args...); delegated || err != nil {
				return err
			}
			return runMigrateStatus(ctx, b, cfgFile, dir)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().StringVar(&dir, "dir", "migrations", "directory holding the migration files")

	return cmd
}

func (b siteBuilder) migratorSite(cfgFile string) (dal.Migrator, *orjanda.Site, error) {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return nil, nil, err
	}
	ms, ok := site.DB.(migratorSource)
	if !ok {
		return nil, nil, errf("config: database.driver %q does not expose a migrator connection", site.Config.Database.Driver)
	}
	return dal.NewMigrator(ms.Dialect(), ms.Underlying()), site, nil
}

func runMigrateDiff(ctx context.Context, b siteBuilder, cfgFile, dir, dialect string, allowDestructive bool) error {
	if dir == "" {
		dir = "migrations"
	}
	mig, site, err := b.migratorSite(cfgFile)
	if err != nil {
		return err
	}

	ms := site.DB.(migratorSource)
	if dialect != "" && dialect != ms.Dialect().Name() {
		return errf("migrate diff: --dialect %q must match database.driver %q", dialect, ms.Dialect().Name())
	}

	diff, err := mig.Diff(ctx, site.Registry)
	if err != nil {
		return err
	}
	if diff == nil || (len(diff.CreateTables) == 0 && len(diff.AlterTables) == 0) {
		fmt.Println("no schema changes")
		return nil
	}

	filename, err := mig.Write(diff, dir, allowDestructive)
	if err != nil {
		return err
	}
	if filename == "" {
		fmt.Println("no schema changes")
		return nil
	}
	fmt.Println("wrote migration:", filepath.Join(dir, filename))
	return nil
}

func runMigrateUp(ctx context.Context, b siteBuilder, cfgFile, dir string) error {
	if dir == "" {
		dir = "migrations"
	}
	mig, _, err := b.migratorSite(cfgFile)
	if err != nil {
		return err
	}
	if err := mig.Up(ctx, dir); err != nil {
		return err
	}
	fmt.Println("migrations applied")
	return nil
}

func runMigrateStatus(ctx context.Context, b siteBuilder, cfgFile, dir string) error {
	if dir == "" {
		dir = "migrations"
	}
	mig, _, err := b.migratorSite(cfgFile)
	if err != nil {
		return err
	}
	statuses, err := mig.Status(ctx, dir)
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Println("no migrations found in " + dir)
		return nil
	}
	fmt.Printf("%-16s %-40s %s\n", "VERSION", "NAME", "APPLIED")
	for _, s := range statuses {
		fmt.Printf("%-16d %-40s %s\n", s.Version, s.Name, boolStr(s.Applied))
	}
	return nil
}
