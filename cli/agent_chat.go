package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/postgres"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// newAgentChatCmd builds the `orjanda agent chat` terminal-mode entry point
// (TAD §16 / PRD §21). It drives the same Runtime.Execute loop the WebSocket
// endpoint drives, printing streaming events inline instead of over the wire.
func newAgentChatCmd() *cobra.Command {
	var cfgFile, user, model string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Terminal-mode agent chat against the local site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgentChat(cmd.Context(), cfgFile, user, model)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().StringVar(&user, "user", "admin", "identity to impersonate")
	cmd.Flags().StringVar(&model, "model", "", "LLM model override (defaults to the provider's configured model)")

	return cmd
}

func runAgentChat(ctx context.Context, cfgFile, user, model string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	opts, err := chatRuntimeOptions(ctx, cfg, model)
	if err != nil {
		return err
	}
	opts.Approvals = &terminalApproval{r: bufio.NewReader(os.Stdin)}
	rt, err := runtime.NewRuntime(opts)
	if err != nil {
		return err
	}

	id := auth.Identity{UserID: user, Roles: []string{"System Administrator"}}
	chatCtx := auth.NewContext(ctx, id)

	fmt.Printf("Orjanda agent chat — impersonating %q (Ctrl-D to exit)\n", user)
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("you> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if _, err := rt.Execute(chatCtx, line); err != nil {
			fmt.Printf("\nerror> %v\n", err)
		}
		fmt.Println()
	}
	return scanner.Err()
}

// chatRuntimeOptions builds the runtime options for a local site: the core
// Application's documents over the configured database, wired exactly like the
// WebSocket endpoint wires them.
func chatRuntimeOptions(ctx context.Context, cfg *config.Config, model string) (runtime.Options, error) {
	var opts runtime.Options
	rawDB, err := openDatabase(cfg.Database)
	if err != nil {
		return opts, err
	}
	db, ok := rawDB.(tableCreater)
	if !ok {
		return opts, fmt.Errorf("config: database driver %q does not support table creation", cfg.Database.Driver)
	}

	reg := schema.NewRegistry()
	for _, d := range []schema.Document{&core.User{}, &core.Role{}, &core.RolePermission{}} {
		if err := reg.Register("core", d); err != nil {
			return opts, err
		}
	}
	if err := reg.Compile(); err != nil {
		return opts, err
	}
	if err := db.CreateTables(reg.List()); err != nil {
		return opts, err
	}
	db.RegisterDocs(reg.List())

	permEngine := perm.NewEngine(reg)
	wfEngine := workflow.NewEngine(db, reg, permEngine, nil, nil)

	tr := tools.NewToolRegistry(permEngine, wfEngine)
	if err := tr.Compile(reg); err != nil {
		return opts, err
	}

	provider, err := llm.ProviderFromConfig(cfg, model)
	if err != nil {
		return opts, err
	}

	policy := safety.SafetyPolicy{
		AutoApprove:       []string{"read", "search", "list"},
		MaxBulkOperations: cfg.LLM.Safety.MaxBulkOperations,
		RateLimit:         safety.RateLimit{OperationsPerMinute: 60, Scope: "user"},
	}

	opts = runtime.Options{
		Provider:  provider,
		Registry:  reg,
		DocEngine: document.NewWithServices(db, reg, permEngine, nil, nil),
		Workflow:  wfEngine,
		Safety:    safety.NewLayer(policy, cache.NewLRUStore(1000)),
		Tools:     tr,
	}
	return opts, nil
}

func openDatabase(cfg config.DatabaseConfig) (dal.Database, error) {
	switch cfg.Driver {
	case "sqlite":
		return sqlite.Open(cfg.DSN)
	case "postgres":
		return postgres.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf("config: database.driver %q is not supported; choose postgres or sqlite", cfg.Driver)
	}
}

// tableCreater is a dal.Database that can also create and register its tables.
// Both concrete dialects implement it; the interfaces are kept separate in the
// TAD on purpose (schema management goes through the Migrator in production).
type tableCreater interface {
	dal.Database
	CreateTables(docs []*schema.CompiledDoc) error
	RegisterDocs(docs []*schema.CompiledDoc)
}

// terminalApproval prompts the operator for every approval_required round trip
// (TAD §12.3), accepting y/n.
type terminalApproval struct {
	r *bufio.Reader
}

func (t *terminalApproval) RequestApproval(_ context.Context, req runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	d := req.Details
	fmt.Printf("\napproval required [%s] %s on %s (policy: %s) — approve? (y/n) ",
		d.Action, d.DocType, d.Action, d.PolicyReason)
	line, _ := t.r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "y" || line == "yes" {
		return runtime.ApprovalResponse{ActionID: req.ActionID, Approved: true}, nil
	}
	return runtime.ApprovalResponse{ActionID: req.ActionID, Approved: false}, nil
}
