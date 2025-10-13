package httpserver

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
)

//go:embed assets/*
var embeddedAssets embed.FS

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveIndex := func() {
			data, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "missing index asset", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if _, err := w.Write(data); err != nil {
				// log write failure if desired
			}
		}

		if r.URL.Path == "/" || r.URL.Path == "" {
			serveIndex()
			return
		}

		normalized := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if normalized == "" {
			serveIndex()
			return
		}

		if _, err := fs.Stat(sub, normalized); err == nil {
			// Reconstruct request to match filesystem expectations.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = cloneURL(r.URL)
			r2.URL.Path = "/" + normalized
			fileServer.ServeHTTP(w, r2)
			return
		}

		http.NotFound(w, r)
	})
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{Path: "/"}
	}
	copy := *u
	return &copy
}
