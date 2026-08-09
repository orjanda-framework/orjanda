package render

import (
	"encoding/json"
	"net/http"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// ResponseEnvelope represents the standardized JSON response envelope per PRD §14.5.
type ResponseEnvelope struct {
	Data  any          `json:"data"`
	Meta  *MetaDetails `json:"meta"`
	Error *ErrorDetail `json:"error"`
}

type MetaDetails struct {
	TotalCount int `json:"total_count,omitempty"`
	Limit      int `json:"limit,omitempty"`
	Offset     int `json:"offset,omitempty"`
}

type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// RespondJSON serializes data and optional metadata into the standard ResponseEnvelope.
func RespondJSON(w http.ResponseWriter, status int, data any, meta *MetaDetails) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	env := ResponseEnvelope{
		Data: data,
		Meta: meta,
	}
	_ = json.NewEncoder(w).Encode(env)
}

// RespondError serializes an error into the standard ResponseEnvelope with proper HTTP status.
func RespondError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	var orjErr orjerrors.Error
	if !orjerrors.As(err, &orjErr) {
		orjErr = orjerrors.Internal("an unexpected error occurred", err)
	}

	status := orjErr.Code().HTTPStatus()
	w.WriteHeader(status)

	env := ResponseEnvelope{
		Error: &ErrorDetail{
			Code:    string(orjErr.Code()),
			Message: orjErr.Message(),
			Details: orjErr.Details(),
		},
	}
	_ = json.NewEncoder(w).Encode(env)
}
