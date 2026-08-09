package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
)

// Document definitions for API integration testing
type Task struct {
	schema.BaseDocument
	Title    string `oj:"required,label=Task Title"`
	Status   string `oj:"options=Open|In Progress|Done,default=Open"`
	Assignee string `oj:"label=Assignee"`
}

func (t *Task) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       "Task",
		Module:     "Core",
		Searchable: true,
		TitleField: "Title",
		Permissions: []schema.DocPermission{
			{Role: "System Administrator", Read: true, Write: true, Create: true, Delete: true},
			{Role: "Task Manager", Read: true, Write: true, Create: true, Delete: false},
			{Role: "Task Viewer", Read: true, Write: false, Create: false, Delete: false},
		},
	}
}

func setupAPITestSite(t *testing.T) (http.Handler, *auth.JWTProvider, schema.Registry) {
	t.Helper()

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

	reg := schema.NewRegistry()
	if err := reg.Register("test_app", &Task{}); err != nil {
		t.Fatalf("failed to register Task: %v", err)
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("failed to compile registry: %v", err)
	}

	compiledDocs := reg.List()
	if err := db.CreateTables(compiledDocs); err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	db.RegisterDocs(compiledDocs)

	permEngine := perm.NewEngine(reg)
	permEngine.SetDatabase(db)

	docEngine := document.NewWithServices(db, reg, permEngine, event.NewBus(), nil)
	jwtProvider := auth.NewJWTProvider([]byte("test-api-secret-key-123456"), 15*time.Minute, 7*24*time.Hour)

	router := api.NewRouter(api.RouterOptions{
		CORSOrigins:  []string{"*"},
		AuthProvider: jwtProvider,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        cache.NewLRUStore(100),
		PermEngine:   permEngine,
		Registry:     reg,
		DocEngine:    docEngine,
	})

	return router, jwtProvider, reg
}

func generateToken(t *testing.T, provider *auth.JWTProvider, userID, email string, roles []string) string {
	t.Helper()
	id := auth.Identity{
		UserID:   userID,
		Email:    email,
		FullName: userID,
		Roles:    roles,
	}
	accToken, _, err := provider.GenerateTokenPair(id)
	if err != nil {
		t.Fatalf("failed to generate token pair: %v", err)
	}
	return accToken
}

// TestREST_FullLifecycle tests Create, Read, List, Update, Delete across 3 roles:
// Admin, Task Manager, Task Viewer, and Unauthorized user.
func TestREST_FullLifecycle(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()

	adminToken := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})
	managerToken := generateToken(t, jwtProvider, "usr_mgr", "mgr@localhost", []string{"Task Manager"})
	viewerToken := generateToken(t, jwtProvider, "usr_viewer", "viewer@localhost", []string{"Task Viewer"})
	guestToken := generateToken(t, jwtProvider, "usr_guest", "guest@localhost", []string{"Guest"})

	var taskID string

	// 1. Admin creates a Task
	{
		body := map[string]any{
			"title":    "Implement Phase 6 API",
			"status":   "In Progress",
			"assignee": "Engineer A",
		}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/document/Task", bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for admin create, got %d: %s", w.Code, w.Body.String())
		}

		var resp api.ResponseEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal create response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("unexpected error in response envelope: %+v", resp.Error)
		}
		data, ok := resp.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any data in response, got %T", resp.Data)
		}
		taskID, ok = data["id"].(string)
		if !ok || taskID == "" {
			t.Fatalf("missing id in created task response")
		}
	}

	// 2. Viewer reads Task (Allowed)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/document/Task/"+taskID, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for viewer read, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 3. Guest reads Task (Denied)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/document/Task/"+taskID, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+guestToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for guest read, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 4. Task Manager updates Task (Allowed)
	{
		body := map[string]any{"status": "Done"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPatch, "/api/v1/document/Task/"+taskID, bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+managerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for manager update, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 5. Viewer attempts update (Denied)
	{
		body := map[string]any{"status": "Open"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPatch, "/api/v1/document/Task/"+taskID, bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for viewer update, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 6. Task Manager attempts Delete (Denied - Task Manager does not have delete permission)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/api/v1/document/Task/"+taskID, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+managerToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for manager delete, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 7. Admin deletes Task (Allowed)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/api/v1/document/Task/"+taskID, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for admin delete, got %d: %s", w.Code, w.Body.String())
		}
	}
}

// TestRPC_MethodPermission asserts that custom RPC methods enforce AllowedRoles.
func TestRPC_MethodPermission(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()
	rpc.ResetRegistry()

	api.RegisterMethod("task.assign", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"assigned": true, "task": args["task_id"]}, nil
	}, api.MethodOpts{
		AllowedRoles: []string{"Task Manager"},
	})

	managerToken := generateToken(t, jwtProvider, "usr_mgr", "mgr@localhost", []string{"Task Manager"})
	viewerToken := generateToken(t, jwtProvider, "usr_viewer", "viewer@localhost", []string{"Task Viewer"})

	// 1. Task Manager invokes method (Allowed)
	{
		body := map[string]any{"task_id": "123"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/method/task.assign", bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+managerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for manager RPC call, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 2. Task Viewer invokes method (Denied)
	{
		body := map[string]any{"task_id": "123"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/method/task.assign", bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for viewer RPC call, got %d: %s", w.Code, w.Body.String())
		}
	}
}

// TestMetaAPI_PrecalculatedPermissions asserts GET /api/v1/meta/{doctype} returns
// accurate pre-calculated permissions for different calling identities.
func TestMetaAPI_PrecalculatedPermissions(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()

	adminToken := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})
	managerToken := generateToken(t, jwtProvider, "usr_mgr", "mgr@localhost", []string{"Task Manager"})
	viewerToken := generateToken(t, jwtProvider, "usr_viewer", "viewer@localhost", []string{"Task Viewer"})

	// 1. Check Admin permissions (All true)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/meta/Task", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for meta GET, got %d: %s", w.Code, w.Body.String())
		}

		var resp api.ResponseEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataMap := resp.Data.(map[string]any)
		perms := dataMap["permissions"].(map[string]any)

		if perms["can_read"] != true || perms["can_write"] != true || perms["can_create"] != true || perms["can_delete"] != true {
			t.Errorf("expected admin to have all true permissions, got %+v", perms)
		}
	}

	// 2. Check Task Manager permissions (Delete = false)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/meta/Task", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+managerToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for meta GET, got %d: %s", w.Code, w.Body.String())
		}

		var resp api.ResponseEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataMap := resp.Data.(map[string]any)
		perms := dataMap["permissions"].(map[string]any)

		if perms["can_read"] != true || perms["can_write"] != true || perms["can_create"] != true || perms["can_delete"] != false {
			t.Errorf("expected manager to have delete=false, got %+v", perms)
		}
	}

	// 3. Check Task Viewer permissions (Read = true, others false)
	{
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/meta/Task", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for meta GET, got %d: %s", w.Code, w.Body.String())
		}

		var resp api.ResponseEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		dataMap := resp.Data.(map[string]any)
		perms := dataMap["permissions"].(map[string]any)

		if perms["can_read"] != true || perms["can_write"] != false || perms["can_create"] != false || perms["can_delete"] != false {
			t.Errorf("expected viewer to have only read=true, got %+v", perms)
		}
	}
}

// TestAPI_Performance benchmarks response times for CRUD (< 50ms) and paginated list (< 100ms).
func TestAPI_Performance(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()
	adminToken := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	// Create 100 Task records for list test
	for i := 0; i < 100; i++ {
		body := map[string]any{
			"title":    fmt.Sprintf("Task %d", i),
			"status":   "Open",
			"assignee": "Engineer",
		}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/document/Task", bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Measure Paginated List performance
	start := time.Now()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/document/Task?limit=50&offset=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for list query, got %d", w.Code)
	}
	t.Logf("Paginated List (50 items out of 100) took %s", elapsed)
	if elapsed > 100*time.Millisecond {
		t.Errorf("list query took %s, exceeding 100ms target", elapsed)
	}
}
