package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

func newRegistryCmd(b siteBuilder) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Inspect the compiled Registry",
		Long:  "List and describe registered Documents (TAD §16). --json feeds the TypeScript codegen pipeline (§6.3).",
	}
	cmd.AddCommand(
		newRegistryListCmd(b),
		newRegistryDescribeCmd(b),
	)
	return cmd
}

func newRegistryListCmd(b siteBuilder) *cobra.Command {
	var cfgFile string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered Documents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := []string{"list", "--config", cfgFile, "--json=" + strconv.FormatBool(asJSON)}
			if delegated, err := b.delegateToApp(ctx, "registry", args...); delegated || err != nil {
				return err
			}
			return runRegistryList(ctx, b, cfgFile, asJSON)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON identical in shape to GET /api/v1/meta")

	return cmd
}

func newRegistryDescribeCmd(b siteBuilder) *cobra.Command {
	var cfgFile string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "describe <doc>",
		Short: "Show full schema for a Document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cliArgs := []string{"describe", args[0], "--config", cfgFile, "--json=" + strconv.FormatBool(asJSON)}
			if delegated, err := b.delegateToApp(ctx, "registry", cliArgs...); delegated || err != nil {
				return err
			}
			return runRegistryDescribe(ctx, b, cfgFile, args[0], asJSON)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the Document in the shape consumed by @orjanda/codegen (ui.CodegenInput element)")

	return cmd
}

func runRegistryList(ctx context.Context, b siteBuilder, cfgFile string, asJSON bool) error {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return err
	}

	docs := site.Registry.List()
	if asJSON {
		summaries := make([]map[string]any, 0, len(docs))
		for _, d := range docs {
			summaries = append(summaries, map[string]any{
				"name":        d.Name,
				"module":      d.Module,
				"title_field": d.TitleField,
				"searchable":  d.Searchable,
				"submittable": d.Submittable,
				"icon":        d.Icon,
				"description": d.Description,
			})
		}
		return emitJSON(summaries)
	}

	for _, d := range docs {
		fmt.Printf("%-20s %-24s %-12s %-14s %4d fields\n", d.Name, d.TableName, d.Module, d.App, len(d.Fields))
	}
	return nil
}

func runRegistryDescribe(ctx context.Context, b siteBuilder, cfgFile, name string, asJSON bool) error {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return err
	}

	if asJSON {
		// Reuse the exact codegen input shape (TAD §6.3 / ui.CodegenInput) so
		// the @orjanda/codegen pipeline consumes describe output unchanged.
		input, err := ui.CodegenInput(site.Registry)
		if err != nil {
			return err
		}
		for _, doc := range input {
			if doc.Name == name {
				return emitJSON(doc)
			}
		}
		return errf("DocType %q not found in Registry", name)
	}

	doc, err := site.Registry.Get(name)
	if err != nil {
		return err
	}
	fmt.Printf("DocType:     %s\n", doc.Name)
	fmt.Printf("App:         %s\n", doc.App)
	fmt.Printf("Module:      %s\n", doc.Module)
	fmt.Printf("Table:       %s\n", doc.TableName)
	fmt.Printf("Searchable:  %t\n", doc.Searchable)
	fmt.Printf("Submittable: %t\n", doc.Submittable)
	fmt.Printf("Description: %s\n", doc.Description)
	fmt.Println("Fields:")
	for _, f := range doc.Fields {
		fmt.Printf("  %-16s %-14s %-10s required=%t unique=%t%s\n",
			f.Name, f.DBColumn, f.Type, f.Required, f.Unique, optionsSuffix(f))
	}
	for _, child := range doc.ChildTables {
		fmt.Printf("Child table: %s (%s)\n", child.FieldName, child.DocType)
		for _, f := range child.Fields {
			fmt.Printf("  %-16s %-14s %-10s required=%t\n", f.Name, f.DBColumn, f.Type, f.Required)
		}
	}
	return nil
}

func optionsSuffix(f schema.Field) string {
	if len(f.Options) > 0 {
		return fmt.Sprintf(" options=%v", f.Options)
	}
	return ""
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
