package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
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

// ConfidentialNote carries a field gated behind oj:"permission=role" so the
// meta endpoint's field-level filtering and read gate can be exercised
// (REVIEW-2026-08-12 finding 8).
type ConfidentialNote struct {
	schema.BaseDocument
	Subject string
	Payload string `oj:"permission=HR Manager"`
}

func (c *ConfidentialNote) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "ConfidentialNote",
		Permissions: []schema.DocPermission{
			{Role: "System Administrator", Read: true, Write: true, Create: true},
			{Role: "Task Manager", Read: true, Write: true, Create: true},
			{Role: "HR Manager", Read: true, Write: true, Create: true},
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
	if err := reg.Register("test_app", &ConfidentialNote{}); err != nil {
		t.Fatalf("failed to register ConfidentialNote: %v", err)
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

// TestMetaAPI_GatedFieldsAndReadGate asserts the meta endpoint does not leak
// gated field metadata (REVIEW-2026-08-12 finding 8): a caller without read
// access gets 403 instead of the full schema; a reader who lacks a gated
// field's role does not see the field; and no field metadata ever echoes the
// required role string.
func TestMetaAPI_GatedFieldsAndReadGate(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()

	managerToken := generateToken(t, jwtProvider, "usr_mgr", "mgr@localhost", []string{"Task Manager"})
	hrToken := generateToken(t, jwtProvider, "usr_hr", "hr@localhost", []string{"HR Manager"})
	outsiderToken := generateToken(t, jwtProvider, "usr_out", "out@localhost", []string{"Unrelated Role"})

	getMeta := func(token string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/meta/ConfidentialNote", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var resp api.ResponseEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data, _ := resp.Data.(map[string]any)
		return w.Code, data
	}

	assertNoRoleStrings := func(fields []any) {
		t.Helper()
		for _, f := range fields {
			fm := f.(map[string]any)
			if _, has := fm["permission"]; has {
				t.Errorf("field %q leaks its required role string", fm["name"])
			}
		}
	}

	// 1. Caller without read access → 403, no schema served.
	code, _ := getMeta(outsiderToken)
	if code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-reader meta GET, got %d", code)
	}

	// 2. Reader lacking the gated field's role sees no gated field.
	code, data := getMeta(managerToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200 for reader meta GET, got %d", code)
	}
	fields, _ := data["fields"].([]any)
	for _, f := range fields {
		fm := f.(map[string]any)
		if fm["name"] == "Payload" {
			t.Error("gated field Payload leaked to a caller without the HR Manager role")
		}
	}
	assertNoRoleStrings(fields)

	// 3. A role-holder sees the gated field — still without the role string.
	code, data = getMeta(hrToken)
	if code != http.StatusOK {
		t.Fatalf("expected 200 for HR meta GET, got %d", code)
	}
	fields, _ = data["fields"].([]any)
	sawPayload := false
	for _, f := range fields {
		fm := f.(map[string]any)
		if fm["name"] == "Payload" {
			sawPayload = true
		}
	}
	if !sawPayload {
		t.Error("HR Manager should see the gated field Payload")
	}
	assertNoRoleStrings(fields)
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

// TestREST_List_OrderByValidation verifies the public order_by query parameter
// is allowlisted (REVIEW-2026-08-12 finding 10): valid field/column + ASC/DESC
// succeed, unknown fields / directions / injected tokens return 400.
func TestREST_List_OrderByValidation(t *testing.T) {
	handler, jwtProvider, _ := setupAPITestSite(t)
	ctx := context.Background()
	adminToken := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	create := func(title string) {
		t.Helper()
		body := map[string]any{"title": title, "status": "Open"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/document/Task", bytes.NewReader(jsonBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
	}
	create("beta")
	create("alpha")
	create("gamma")

	list := func(orderBy string) (*httptest.ResponseRecorder, api.ResponseEnvelope) {
		t.Helper()
		listURL := "/api/v1/document/Task"
		if orderBy != "" {
			listURL += "?order_by=" + url.QueryEscape(orderBy)
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, listURL, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var resp api.ResponseEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w, resp
	}

	t.Run("valid field asc", func(t *testing.T) {
		w, resp := list("title")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		rows := resp.Data.([]any)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		first := rows[0].(map[string]any)
		if first["title"] != "alpha" {
			t.Fatalf("expected first title alpha, got %v", first["title"])
		}
	})

	t.Run("valid field desc", func(t *testing.T) {
		w, resp := list("title DESC")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		rows := resp.Data.([]any)
		first := rows[0].(map[string]any)
		if first["title"] != "gamma" {
			t.Fatalf("expected first title gamma, got %v", first["title"])
		}
	})

	t.Run("valid system column", func(t *testing.T) {
		w, _ := list("created_at")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for created_at order_by, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		w, resp := list("nosuchfield")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if resp.Error == nil || resp.Error.Code != string(orjerrors.CodeValidation) {
			t.Fatalf("expected VALIDATION_ERROR, got %+v", resp.Error)
		}
	})

	t.Run("unknown direction rejected", func(t *testing.T) {
		w, _ := list("title SENSITIVE")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("injection rejected", func(t *testing.T) {
		w, _ := list("title DESC; DROP TABLE tasks; --")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}
