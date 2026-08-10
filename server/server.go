package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/orjanda-framework/orjanda"
)

// Assemble returns the composed HTTP handler (API + embedded Admin UI) for the
// given Site.
func Assemble(site *orjanda.Site) http.Handler {
	return site.HTTPHandler()
}

// Run starts the HTTP server for site and blocks until context cancellation or error.
func Run(ctx context.Context, site *orjanda.Site) error {
	port := site.Config.Server.Port
	if port == 0 {
		port = 8080
	}
	host := site.Config.Server.Host
	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	srv := &http.Server{
		Addr:    addr,
		Handler: site.HTTPHandler(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
