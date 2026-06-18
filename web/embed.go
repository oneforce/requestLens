package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html assets
var content embed.FS

func NewHandler() http.Handler {
	sub, err := fs.Sub(content, ".")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" || r.URL.Path == "/favicon.ico" {
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}
		files.ServeHTTP(w, r)
	})
}
