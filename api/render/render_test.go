package render_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orjanda-framework/orjanda/api/render"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

func TestRespondJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"foo": "bar"}
	meta := &render.MetaDetails{TotalCount: 1, Limit: 10, Offset: 0}

	render.RespondJSON(w, http.StatusOK, data, meta)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var env render.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if env.Error != nil {
		t.Errorf("expected error to be nil, got %+v", env.Error)
	}
	if env.Meta == nil || env.Meta.TotalCount != 1 {
		t.Errorf("expected meta total_count 1, got %+v", env.Meta)
	}
}

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	err := orjerrors.Permission("access denied to document")

	render.RespondError(w, err)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var env render.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if env.Error == nil {
		t.Fatalf("expected error object in envelope, got nil")
	}
	if env.Error.Code != "PERMISSION_DENIED" {
		t.Errorf("expected code PERMISSION_DENIED, got %s", env.Error.Code)
	}
}
