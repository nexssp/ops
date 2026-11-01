package studio

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

//go:embed web/dist/*
var studioWeb embed.FS

func studioUIAction(s *Service) action.AnyAction {
	return action.New("studio.ui", func(_ context.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	}).System().Tag("infra", "studio").
		Route(thttp.RawHandler(http.MethodGet, "/studio", s.serveStudio)).
		Route(thttp.RawHandler(http.MethodGet, "/studio/", s.serveStudio)).
		Build()
}

func (s *Service) serveStudio(w http.ResponseWriter, r *http.Request) {
	staticFiles, err := fs.Sub(studioWeb, "web/dist")
	if err != nil {
		http.Error(w, "Studio assets unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'self'")
	if r.URL.Path == "/studio" {
		http.Redirect(w, r, "/studio/", http.StatusTemporaryRedirect)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/studio/") {
		http.NotFound(w, r)
		return
	}
	http.StripPrefix("/studio/", http.FileServer(http.FS(staticFiles))).ServeHTTP(w, r)
}
