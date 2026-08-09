package api

import (
	"net/http"

	"github.com/orjanda-framework/orjanda/api/render"
)

type ResponseEnvelope = render.ResponseEnvelope
type MetaDetails = render.MetaDetails
type ErrorDetail = render.ErrorDetail

func RespondJSON(w http.ResponseWriter, status int, data any, meta *MetaDetails) {
	render.RespondJSON(w, status, data, meta)
}

func RespondError(w http.ResponseWriter, err error) {
	render.RespondError(w, err)
}
