package orjanda

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

// uiDist embeds the production Vite build of the Admin UI into the single Go
// binary (PRD §17.4). The dist directory ships in the repository (a placeholder
// index.html is committed) so `go build` never depends on a frontend deploy
// step; `npm run build` overwrites it.
//
//go:embed all:orjanda-ui/dist
var uiDist embed.FS

// spaHandler serves the Admin UI SPA: /api/* traffic goes to the API router,
// everything else resolves against the embedded dist with an index.html
// fallback so client-side routes (/doc/Employee, /agent, ...) work directly.
type spaHandler struct {
	api   http.Handler
	dist  fs.FS
	index []byte
}

func newSPAHandler(api http.Handler) *spaHandler {
	h := &spaHandler{api: api, dist: uiDist}
	index, err := fs.ReadFile(uiDist, "orjanda-ui/dist/index.html")
	if err != nil {
		return h
	}
	h.index = index
	return h
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.api.ServeHTTP(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	if data, err := fs.ReadFile(h.dist, "orjanda-ui/dist/"+name); err == nil {
		w.Header().Set("Content-Type", contentTypeFor(name))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// SPA fallback: unknown paths are client routes, serve index.html.
	if h.index == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.index)
}

func contentTypeFor(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
