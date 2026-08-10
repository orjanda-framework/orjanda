package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	core "github.com/orjanda-framework/orjanda/orjanda-core"
)

func newConsoleCmd(b siteBuilder) *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "console",
		Short: "Interactive REPL with site context",
		Long:  "REPL wrapping the constructed *orjanda.Site (TAD §16). Type `help` for commands, `exit` to quit.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if delegated, err := b.delegateToApp(ctx, "console", forwardConfig(cfgFile)...); delegated || err != nil {
				return err
			}
			return runConsole(ctx, b, cfgFile)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")

	return cmd
}

func runConsole(ctx context.Context, b siteBuilder, cfgFile string) error {
	site, _, err := b.loadSite(cfgFile)
	if err != nil {
		return err
	}

	fmt.Println("Orjanda console — site context ready (" + serveAddr(site.Config) + ")")
	fmt.Println("Type `help` for commands, `exit` to quit.")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("orjanda> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "exit", "quit":
			return nil
		case "help":
			consoleHelp()
		case "docs", "list":
			for _, d := range site.Registry.List() {
				fmt.Printf("  %-20s %-24s %-12s %4d fields\n", d.Name, d.TableName, d.Module, len(d.Fields))
			}
		case "describe":
			if len(fields) < 2 {
				fmt.Println("usage: describe <doc>")
				continue
			}
			doc, err := site.Registry.Get(fields[1])
			if err != nil {
				fmt.Println("error:", err)
				continue
			}
			fmt.Printf("  %s (%s, module %s)\n", doc.Name, doc.TableName, doc.Module)
			for _, f := range doc.Fields {
				fmt.Printf("    %-16s %-10s required=%t\n", f.Name, f.Type, f.Required)
			}
		case "bootstrap":
			pw, err := core.Bootstrap(ctx, site.DB, site.Registry)
			if err != nil {
				fmt.Println("error:", err)
				continue
			}
			if pw == "" {
				fmt.Println("already bootstrapped")
			} else {
				fmt.Printf("bootstrapped admin: %s / %s\n", core.AdminEmail, pw)
			}
		case "install", "uninstall":
			if len(fields) < 2 {
				fmt.Printf("usage: %s <app>\n", fields[0])
				continue
			}
			if fields[0] == "install" {
				if err := runInstall(ctx, b, cfgFile, fields[1]); err != nil {
					fmt.Println("error:", err)
				}
			} else {
				if err := runUninstall(ctx, b, cfgFile, fields[1], false); err != nil {
					fmt.Println("error:", err)
				}
			}
		default:
			fmt.Printf("unknown command %q — type `help`\n", fields[0])
		}
	}
	return scanner.Err()
}

func consoleHelp() {
	fmt.Println("  list | docs             list registered Documents")
	fmt.Println("  describe <doc>          show a Document's fields")
	fmt.Println("  bootstrap               first-run admin bootstrap")
	fmt.Println("  install <app>           run an app's OnInstall hook")
	fmt.Println("  uninstall <app>         run an app's OnUninstall hook")
	fmt.Println("  exit | quit             leave the console")
}
