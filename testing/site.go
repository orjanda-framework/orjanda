package testing

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
	"github.com/stretchr/testify/require"
)

// TestSite is the test-site composition root returned by NewTestSite. It
// embeds the full *orjanda.Site and additionally exposes the Document Engine
// and an Agent Runtime under the names PRD §32 uses (site.Document.Create,
// site.Agent.Execute), so the §32.2/§32.3 acceptance examples read verbatim.
// See TAD §17.
type TestSite struct {
	*orjanda.Site

	// Document is the same Document Engine the site's DocEngine field
	// references. Every Create/Read/Update/Delete call goes through the real
	// permission, hook, workflow, and audit pipeline — no test-only bypass
	// (PRD §25.1, TAD §17.1 guarantee 3).
	Document *document.Engine

	// Agent is an Agent Runtime wired to the site's engines. No LLM provider
	// is configured by default: a turn only works when the caller supplies one
	// per call via agent.WithProvider(...) — which is how MockLLM drives it.
	Agent *runtime.Runtime

	t *testing.T
}

// Option configures a NewTestSite call. See TAD §17.
type Option func(*testSiteConfig)

type testSiteConfig struct {
	dialect string
	apps    []app.Definition
	regs    []appDocs
}

type appDocs struct {
	app  string
	docs []schema.Document
}

// WithApps installs the given Application definitions onto the site, ordered
// by the application dependency DAG (TAD §7.1 step 2). The Application's
// Documents must be supplied either through its app.Installable.OnInstall hook
// (PRD §11.3 "Install: register documents") or via WithDocuments — the
// app.Definition type itself carries no Documents (TAD §7).
func WithApps(apps ...app.Definition) Option {
	return func(c *testSiteConfig) {
		c.apps = append(c.apps, apps...)
	}
}

// WithDocuments registers Documents under an Application name. Use it to
// declare an Application's Documents to the harness when the Application has
// no OnInstall hook of its own. Documents are registered before the Registry
// is compiled, so they are compiled, have tables created, and are immediately
// usable when NewTestSite returns (TAD §17.1 guarantee 2).
func WithDocuments(appName string, docs ...schema.Document) Option {
	return func(c *testSiteConfig) {
		c.regs = append(c.regs, appDocs{app: appName, docs: docs})
	}
}

// WithDialect selects the backing database. Default is a fresh in-memory
// SQLite database per test. "postgres" opts into a testcontainers-go-backed
// PostgreSQL instance (TAD §17.1 guarantee 1) and requires the "integration"
// build tag; without it NewTestSite fails fast.
func WithDialect(d string) Option {
	return func(c *testSiteConfig) {
		c.dialect = d
	}
}

// NewTestSite provisions a fresh, isolated site for a single test: a new
// in-memory SQLite database by default, the core User/Role/RolePermission
// Documents, and every Document declared via WithDocuments/WithApps installed.
// Registry.Compile() has already run and all tables exist when it returns —
// the site is immediately usable with no serve step (TAD §17.1 guarantees
// 1–3). No HTTP server is started.
func NewTestSite(t *testing.T, opts ...Option) *TestSite {
	t.Helper()

	cfg := testSiteConfig{dialect: "sqlite"}
	for _, opt := range opts {
		opt(&cfg)
	}

	site, err := orjanda.NewSite(config.Config{})
	require.NoError(t, err)

	// The core Application and its Documents are always present (TAD §4).
	allApps := make([]app.Definition, 0, len(cfg.apps)+1)
	allApps = append(allApps, core.App)
	allApps = append(allApps, cfg.apps...)
	ordered, err := app.ResolveDAG(allApps)
	require.NoError(t, err)

	for _, a := range ordered {
		if a.Name == core.App.Name {
			for _, d := range []schema.Document{&core.User{}, &core.Role{}, &core.RolePermission{}} {
				require.NoError(t, site.Registry.Register(a.Name, d))
			}
		}
		if inst, ok := any(a).(app.Installable); ok {
			require.NoError(t, inst.OnInstall(context.Background(), site))
		}
		site.Install(a)
	}

	for _, reg := range cfg.regs {
		for _, d := range reg.docs {
			require.NoError(t, site.Registry.Register(reg.app, d))
		}
	}

	// Core User hooks (bcrypt password hashing, TAD §4.1) — installed before
	// any write so CreateUser stores a hash the login endpoint can verify.
	core.RegisterUserHooks(site.EventBus)

	// Compile the Registry, then open the database and create every table.
	// The DB cannot be opened before compile: RegisterDocs/CreateTables
	// consume compiled Documents (TAD §2.3). Site.Compile() is deliberately
	// not used — it would re-compile the already-compiled Registry — so the
	// engine wiring below mirrors Compile's steps (TAD §5.2) with the DB
	// attached first.
	require.NoError(t, site.Registry.Compile())

	db := openTestDB(t, cfg.dialect, site.Registry.List())
	site.DB = db

	site.Permissions = perm.NewEngine(site.Registry)
	site.Permissions.SetDatabase(db)
	site.DocEngine = document.NewWithServices(
		db, site.Registry, site.Permissions, site.EventBus, site.AuditLog)
	site.Workflows = workflow.NewEngine(
		db, site.Registry, site.Permissions, site.EventBus, site.AuditLog)
	site.Tools = tools.NewToolRegistry(site.Permissions, site.Workflows)
	require.NoError(t, site.Tools.Compile(site.Registry))

	site.Router = api.NewRouter(api.RouterOptions{
		AuthProvider: site.Auth,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        site.Cache,
		PermEngine:   site.Permissions,
		Registry:     site.Registry,
		DocEngine:    site.DocEngine,
		Database:     site.DB,
		Pages:        site.Pages,
	})

	agentRT, err := runtime.NewRuntime(runtime.Options{
		Provider:  errorProvider{},
		Tools:     site.Tools,
		Registry:  site.Registry,
		DocEngine: site.DocEngine,
		Workflow:  site.Workflows,
		Safety:    safety.NewLayer(defaultSafetyPolicy(), site.Cache),
	})
	require.NoError(t, err)

	return &TestSite{Site: site, t: t, Document: site.DocEngine, Agent: agentRT}
}

// defaultSafetyPolicy is the harness's default approval policy (TAD §12).
// Read/search/list are auto-approved; everything else fails closed to
// "requires approval" — which is what lets MockLLM script a plan-level
// ApprovalPrompt exchange without any test-side policy setup (TAD §17.1
// guarantee 4, §11.2 step b).
func defaultSafetyPolicy() safety.SafetyPolicy {
	return safety.SafetyPolicy{
		AutoApprove: []string{"read", "search", "list"},
	}
}

// errorProvider is the Agent Runtime's default provider: it fails every turn
// with an explicit pointer to WithProvider, so a test that calls
// site.Agent.Execute without supplying a MockLLM gets a clear error instead
// of a silent misconfiguration.
type errorProvider struct{}

func (errorProvider) ChatCompletion(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, orjerrors.Internal(
		"testing: no LLM provider configured for site.Agent; pass agent.WithProvider(...) per turn (e.g. orjanda/testing.MockLLM)", nil)
}

func (errorProvider) StreamChatCompletion(context.Context, llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, orjerrors.Internal(
		"testing: no LLM provider configured for site.Agent; pass agent.WithProvider(...) per turn (e.g. orjanda/testing.MockLLM)", nil)
}

func (errorProvider) SupportsToolCalling() bool      { return false }
func (errorProvider) SupportsStructuredOutput() bool { return false }
func (errorProvider) ModelInfo() llm.ModelInfo       { return llm.ModelInfo{Name: "testing:error-provider"} }

// CreateUser provisions a User (plus Role records and UserRole grants) with
// the given roles and returns its auth.Identity. Runs through the real
// Document Engine under the System Administrator identity, so the User,
// UserRole, and Role rows are written through the same permission/audit path
// the application uses (TAD §4.1, PRD §32.2).
func (s *TestSite) CreateUser(t *testing.T, email string, roles ...string) auth.Identity {
	t.Helper()

	name := strings.Split(email, "@")[0]
	for _, role := range roles {
		s.ensureRole(t, sysadminCtx(), role)
	}

	id, err := s.Document.Create(sysadminCtx(), "User", map[string]any{
		"Email":    email,
		"FullName": name,
		"Password": "orjanda-test-password",
		"Active":   true,
	})
	require.NoError(t, err, "create user %q", email)

	for i, role := range roles {
		_, err := s.DB.Insert(sysadminCtx(), "UserRole", map[string]any{
			"parent_id": id,
			"role":      role,
			"idx":       i,
		})
		require.NoError(t, err, "grant role %q to %q", role, email)
	}

	return auth.Identity{UserID: id, Email: email, FullName: name, Roles: roles}
}

// ensureRole creates the Role record if it does not already exist.
func (s *TestSite) ensureRole(t *testing.T, ctx context.Context, role string) {
	t.Helper()
	rows, err := s.DB.Query(ctx, dal.Select{
		DocType: "Role",
		Filters: map[string]any{"role_name": role},
	})
	require.NoError(t, err)
	if len(rows) > 0 {
		return
	}
	_, err = s.Document.Create(ctx, "Role", map[string]any{"RoleName": role})
	require.NoError(t, err, "create role %q", role)
}

// WithUser returns a context carrying id, ready to pass to Document Engine or
// Agent calls (TAD §1.2 — identity travels only via context).
func (s *TestSite) WithUser(id auth.Identity) context.Context {
	return auth.NewContext(context.Background(), id)
}

// SeedFixtures loads Application fixture JSON (PRD §11.1 fixtures/) and creates
// every record through the Document Engine under the System Administrator
// identity. The file is a map of DocType → array of record objects, e.g.:
//
//	{ "Employee": [ {"FirstName":"Ada","LastName":"Lovelace"} ] }
//
// Records are created in file order per DocType (map iteration order across
// DocTypes is unspecified; fixtures must not depend on cross-DocType order).
func (s *TestSite) SeedFixtures(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read fixture %q", path)

	var fixtures map[string][]map[string]any
	require.NoError(t, json.Unmarshal(raw, &fixtures), "parse fixture %q", path)

	for docType, records := range fixtures {
		for _, rec := range records {
			_, err := s.Document.Create(sysadminCtx(), docType, rec)
			require.NoError(t, err, "seed %s fixture %v", docType, rec)
		}
	}
}

// sysadminCtx is the privileged identity used for harness-internal writes
// (CreateUser, SeedFixtures, role provisioning). CheckAction grants
// System Administrator every action (TAD §2.4).
func sysadminCtx() context.Context {
	return auth.NewContext(context.Background(), auth.Identity{Roles: []string{"System Administrator"}})
}

// openTestDB opens a fresh database for one test and creates every table for
// docs. See TAD §17.1 guarantee 1.
func openTestDB(t *testing.T, dialect string, docs []*schema.CompiledDoc) dal.Database {
	t.Helper()
	switch dialect {
	case "sqlite":
		return openSQLiteTestDB(t, docs)
	case "postgres":
		return openPostgresTestDB(t, docs)
	default:
		t.Fatalf("testing: unknown dialect %q (want \"sqlite\" or \"postgres\")", dialect)
		return nil
	}
}

// openSQLiteTestDB opens a fresh in-memory SQLite database. MaxOpenConns(1)
// pins the single shared connection that an in-memory database requires.
func openSQLiteTestDB(t *testing.T, docs []*schema.CompiledDoc) dal.Database {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.Underlying().SetMaxOpenConns(1)
	db.RegisterDocs(docs)
	require.NoError(t, db.CreateTables(docs))
	return db
}
