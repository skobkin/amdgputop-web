package httpserver

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"path"
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
		if r.URL.Path == "/" || r.URL.Path == "" {
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = cloneURL(r.URL)
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}

		// Serve static files for exact matches; fallback to index.html otherwise.
		normalized := path.Clean(r.URL.Path)
		if normalized == "/" {
			normalized = "/index.html"
		}

		f, err := sub.Open(normalized[1:])
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		r2 := new(http.Request)
		*r2 = *r
		r2.URL = cloneURL(r.URL)
		r2.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r2)
	})
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{Path: "/"}
	}
	copy := *u
	return &copy
}
