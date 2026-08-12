package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	orjtesting "github.com/orjanda-framework/orjanda/testing"
	"github.com/stretchr/testify/require"
)

// restLogin authenticates email/password against the real login endpoint and
// returns the access token. This exercises the same POST /api/v1/auth/login
// flow a client uses (PRD §15.1) — token minting, bcrypt verification, and
// role loading from the DB.
func restLogin(t *testing.T, site *orjtesting.TestSite, email, password string) string {
	t.Helper()
	res := doJSON(t, site, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": password,
	})
	require.Equal(t, http.StatusOK, res.Code, "login failed: %s", res.Body.String())
	var env struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
		Error any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &env))
	require.NotEmpty(t, env.Data.AccessToken)
	return env.Data.AccessToken
}

// doJSON performs an HTTP request against the site's real router with an
// optional bearer token and JSON body, returning the recorder.
func doJSON(t *testing.T, site *orjtesting.TestSite, method, path, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	site.Router.ServeHTTP(w, req)
	return w
}

// createREST creates docType via POST /api/v1/document/{doctype} and returns
// the new record's id.
func createREST(t *testing.T, site *orjtesting.TestSite, token, docType string, body map[string]any) string {
	t.Helper()
	res := doJSON(t, site, http.MethodPost, "/api/v1/document/"+docType, token, body)
	require.Equalf(t, http.StatusCreated, res.Code, "create %s failed: %s", docType, res.Body.String())
	var env struct {
		Data map[string]any `json:"data"`
		Meta any            `json:"meta"`
		Err  *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &env))
	require.Nil(t, env.Err, "unexpected error creating %s: %s", docType, res.Body.String())
	id, _ := env.Data["id"].(string)
	require.NotEmpty(t, id, "create %s returned no id", docType)
	return id
}

// restCreateCode returns the HTTP status and the envelope error code for an
// attempted create, letting the test assert both the status and the
// standardized error code (TAD §1.1).
func restCreateCode(t *testing.T, site *orjtesting.TestSite, token, docType string, body map[string]any) (int, string) {
	t.Helper()
	res := doJSON(t, site, http.MethodPost, "/api/v1/document/"+docType, token, body)
	var env struct {
		Err *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &env)
	code := ""
	if env.Err != nil {
		code = env.Err.Code
	}
	return res.Code, code
}

// TestREST_FullCRUD_AllFourDocs verifies Phase 12 criterion 3 (PRD §44.4 item
// 3): the REST API supports full CRUD for all four HR Documents, with
// authentication (401 without a token) and permission enforcement for at least
// two roles — hr_manager (full CRUD) and employee (read-only, except creating
// leave requests). Every request goes through the real router, auth
// middleware, permission engine, and Document Engine.
func TestREST_FullCRUD_AllFourDocs(t *testing.T) {
	site := newHRSite(t)

	// Real users created through the Document Engine; tokens come from the
	// real login endpoint so the roles in the JWT are loaded from the DB.
	site.CreateUser(t, "hr@test.com", "hr_manager")
	site.CreateUser(t, "emp@test.com", "employee")
	hrToken := restLogin(t, site, "hr@test.com", "orjanda-test-password")
	empToken := restLogin(t, site, "emp@test.com", "orjanda-test-password")

	// 0. Authentication is enforced. The Auth middleware passes a missing
	// credential through as an anonymous identity so perm.Engine is the single
	// enforcement path (PRD §25.1): anonymous is denied 403. A malformed or
	// cryptographically invalid credential is rejected by the middleware with
	// 401 AUTH_ERROR before any permission evaluation (TAD §1.1).
	{
		res := doJSON(t, site, http.MethodGet, "/api/v1/document/Department", "", nil)
		require.Equal(t, http.StatusForbidden, res.Code,
			"unauthenticated list must be denied, got %d: %s", res.Code, res.Body.String())

		req := httptest.NewRequest(http.MethodGet, "/api/v1/document/Department", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
		w := httptest.NewRecorder()
		site.Router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code,
			"invalid token must be 401, got %d: %s", w.Code, w.Body.String())

		req = httptest.NewRequest(http.MethodGet, "/api/v1/document/Department", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		w = httptest.NewRecorder()
		site.Router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code,
			"malformed auth header must be 401, got %d: %s", w.Code, w.Body.String())
	}

	// 1. hr_manager: full CRUD on all four Documents.
	// Department.
	deptID := createREST(t, site, hrToken, "Department", map[string]any{
		"Name": "Engineering", "Description": "Builds the product",
	})
	// Employee.
	empID := createREST(t, site, hrToken, "Employee", map[string]any{
		"FirstName": "Jane", "LastName": "Doe",
		"Email": "jane@test.com", "Department": deptID, "JoinDate": "2020-01-15",
		"Salary": 75000.0,
	})
	// LeaveType.
	ltID := createREST(t, site, hrToken, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	// LeaveRequest.
	lrID := createREST(t, site, hrToken, "LeaveRequest", map[string]any{
		"Employee": empID, "LeaveType": ltID,
		"FromDate": "2026-08-15", "ToDate": "2026-08-16", "Reason": "Vacation",
	})

	// Read + List each record back.
	for _, tc := range []struct {
		docType, id string
	}{
		{"Department", deptID},
		{"Employee", empID},
		{"LeaveType", ltID},
		{"LeaveRequest", lrID},
	} {
		res := doJSON(t, site, http.MethodGet, "/api/v1/document/"+tc.docType+"/"+tc.id, hrToken, nil)
		require.Equalf(t, http.StatusOK, res.Code, "read %s failed: %s", tc.docType, res.Body.String())

		res = doJSON(t, site, http.MethodGet, "/api/v1/document/"+tc.docType, hrToken, nil)
		require.Equalf(t, http.StatusOK, res.Code, "list %s failed: %s", tc.docType, res.Body.String())
	}

	// Update (PATCH) each record.
	{
		res := doJSON(t, site, http.MethodPatch, "/api/v1/document/Department/"+deptID, hrToken,
			map[string]any{"Description": "Builds everything"})
		require.Equal(t, http.StatusOK, res.Code, "update Department failed: %s", res.Body.String())

		res = doJSON(t, site, http.MethodPatch, "/api/v1/document/Employee/"+empID, hrToken,
			map[string]any{"Salary": 80000.0})
		require.Equal(t, http.StatusOK, res.Code, "update Employee failed: %s", res.Body.String())

		res = doJSON(t, site, http.MethodPatch, "/api/v1/document/LeaveType/"+ltID, hrToken,
			map[string]any{"MaxDaysPerYear": 25})
		require.Equal(t, http.StatusOK, res.Code, "update LeaveType failed: %s", res.Body.String())

		res = doJSON(t, site, http.MethodPatch, "/api/v1/document/LeaveRequest/"+lrID, hrToken,
			map[string]any{"Reason": "Family time"})
		require.Equal(t, http.StatusOK, res.Code, "update LeaveRequest failed: %s", res.Body.String())
	}

	// Verify the update actually persisted.
	{
		res := doJSON(t, site, http.MethodGet, "/api/v1/document/Employee/"+empID, hrToken, nil)
		require.Equal(t, http.StatusOK, res.Code)
		var env struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &env))
		require.Equal(t, 80000.0, env.Data["salary"], "PATCH must persist")
	}

	// Delete each record; the record must then be gone from lists/reads.
	for _, tc := range []struct{ docType, id string }{
		{"LeaveRequest", lrID},
		{"LeaveType", ltID},
		{"Employee", empID},
		{"Department", deptID},
	} {
		res := doJSON(t, site, http.MethodDelete, "/api/v1/document/"+tc.docType+"/"+tc.id, hrToken, nil)
		require.Equalf(t, http.StatusOK, res.Code, "delete %s failed: %s", tc.docType, res.Body.String())

		res = doJSON(t, site, http.MethodGet, "/api/v1/document/"+tc.docType+"/"+tc.id, hrToken, nil)
		require.Equalf(t, http.StatusNotFound, res.Code, "deleted %s must be unreadable, got %d",
			tc.docType, res.Code)
	}

	// 2. employee role: read-only on the four HR Documents, with the one
	// documented exception that employees may create LeaveRequests
	// (modules/leave/documents/leave_request.go permissions).
	//
	// Re-seed a department + employee + leave type as hr_manager so the
	// employee has valid links to reference.
	dept2ID := createREST(t, site, hrToken, "Department", map[string]any{"Name": "Marketing"})
	emp2ID := createREST(t, site, hrToken, "Employee", map[string]any{
		"FirstName": "Bob", "LastName": "Smith",
		"Email": "bob@test.com", "Department": dept2ID, "JoinDate": "2021-05-01",
	})
	lt2ID := createREST(t, site, hrToken, "LeaveType", map[string]any{
		"Name": "Sick", "MaxDaysPerYear": 10, "IsPaid": true,
	})

	// Reads succeed for employee on all four.
	for _, docType := range []string{"Department", "Employee", "LeaveType", "LeaveRequest"} {
		res := doJSON(t, site, http.MethodGet, "/api/v1/document/"+docType, empToken, nil)
		require.Equalf(t, http.StatusOK, res.Code, "employee list %s must be allowed, got %d: %s",
			docType, res.Code, res.Body.String())
	}

	// Writes the employee lacks are denied with 403 + PERMISSION_DENIED.
	for _, tc := range []struct {
		docType string
		body    map[string]any
	}{
		{"Department", map[string]any{"Name": "Rogue Dept"}},
		{"Employee", map[string]any{
			"FirstName": "Eve", "LastName": "Mallory", "Email": "eve@test.com",
		}},
		{"LeaveType", map[string]any{"Name": "Rogue Leave"}},
	} {
		status, code := restCreateCode(t, site, empToken, tc.docType, tc.body)
		require.Equalf(t, http.StatusForbidden, status, "employee create %s must be 403, got %d",
			tc.docType, status)
		require.Equalf(t, "PERMISSION_DENIED", code, "employee create %s must be PERMISSION_DENIED, got %q",
			tc.docType, code)
	}

	// Employee CAN create a leave request (their own request) — the exception.
	empLRID := createREST(t, site, empToken, "LeaveRequest", map[string]any{
		"Employee": emp2ID, "LeaveType": lt2ID,
		"FromDate": "2026-09-01", "ToDate": "2026-09-02", "Reason": "Migraine",
	})

	// Employee cannot update or delete any of the four.
	for _, tc := range []struct{ docType, id string }{
		{"Department", dept2ID},
		{"Employee", emp2ID},
		{"LeaveType", lt2ID},
		{"LeaveRequest", empLRID},
	} {
		res := doJSON(t, site, http.MethodPatch, "/api/v1/document/"+tc.docType+"/"+tc.id, empToken,
			map[string]any{"Name": "Hacked"})
		require.Equalf(t, http.StatusForbidden, res.Code, "employee PATCH %s must be 403, got %d",
			tc.docType, res.Code)

		res = doJSON(t, site, http.MethodDelete, "/api/v1/document/"+tc.docType+"/"+tc.id, empToken, nil)
		require.Equalf(t, http.StatusForbidden, res.Code, "employee DELETE %s must be 403, got %d",
			tc.docType, res.Code)
	}

	// 3. A second role can do everything the first can: prove the two-role
	// comparison is not accidental by confirming hr_manager can still mutate a
	// record the employee just created.
	{
		res := doJSON(t, site, http.MethodPatch, "/api/v1/document/LeaveRequest/"+empLRID, hrToken,
			map[string]any{"Reason": "Approved by HR"})
		require.Equal(t, http.StatusOK, res.Code, "hr_manager PATCH of employee request failed: %s",
			res.Body.String())
	}
}
