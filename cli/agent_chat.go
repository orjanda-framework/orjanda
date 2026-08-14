package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/config"
)

func newAgentCmd(b siteBuilder) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Embedded AI agent tooling",
	}
	cmd.AddCommand(newAgentChatCmd(b))
	return cmd
}

// newAgentChatCmd builds the `orjanda agent chat` terminal-mode entry point
// (TAD §16 / PRD §21). It drives the same Runtime.Execute loop the WebSocket
// endpoint drives, printing streaming events inline instead of over the wire.
func newAgentChatCmd(b siteBuilder) *cobra.Command {
	var cfgFile, user, model string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Terminal-mode agent chat against the local site",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			args := []string{"chat", "--config", cfgFile, "--user", user, "--model", model}
			if delegated, err := b.delegateToApp(ctx, "agent", args...); delegated || err != nil {
				return err
			}
			return runAgentChat(ctx, b, cfgFile, user, model)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "path to orjanda.yaml (defaults to orjanda.yaml in cwd)")
	cmd.Flags().StringVar(&user, "user", "admin", "identity to impersonate")
	cmd.Flags().StringVar(&model, "model", "", "LLM model override (defaults to the provider's configured model)")

	return cmd
}

func runAgentChat(ctx context.Context, b siteBuilder, cfgFile, user, model string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	site, err := b.newSite(*cfg)
	if err != nil {
		return err
	}
	if err := site.Compile(); err != nil {
		return err
	}
	// The chat loop reads/writes Documents, so tables must exist and the
	// docType→table mappings must be wired (same as serve's dev auto-create).
	if tc, ok := site.DB.(tableCreater); ok {
		if err := tc.CreateTables(site.Registry.List()); err != nil {
			return err
		}
		tc.RegisterDocs(site.Registry.List())
	}

	opts, err := runtimeOptionsFromSite(site, model)
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

	// One session per chat invocation: turns share the transcript, seen
	// DocTypes, and target count so the discovery/operation split and bulk
	// approval stay continuous (TAD §11.1/§12.1, REVIEW-2026-08-12 finding 3).
	sess := rt.NewSession(id)
	chatCtx = safety.WithSession(chatCtx, sess.ID)

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

// runtimeOptionsFromSite derives agent runtime options from a compiled site,
// mirroring the WebSocket endpoint's wiring (TAD §12.4).
func runtimeOptionsFromSite(site *orjanda.Site, model string) (runtime.Options, error) {
	provider, err := llm.ProviderFromConfig(&site.Config, model)
	if err != nil {
		return runtime.Options{}, err
	}

	policy := safety.SafetyPolicy{
		AutoApprove:       []string{"read", "search", "list"},
		MaxBulkOperations: site.Config.LLM.Safety.MaxBulkOperations,
		RateLimit:         safety.RateLimit{OperationsPerMinute: 60, Scope: "user"},
	}

	return runtime.Options{
		Provider:   provider,
		Tools:      site.Tools,
		PermEngine: site.Permissions,
		Registry:   site.Registry,
		DocEngine:  site.DocEngine,
		Workflow:   site.Workflows,
		Safety:     safety.NewLayer(policy, cache.NewLRUStore(1000)),
	}, nil
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
