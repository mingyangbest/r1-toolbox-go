package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var distFS embed.FS

func getDistFS() http.FileSystem {
	sub, _ := fs.Sub(distFS, "dist")
	return http.FS(sub)
}
