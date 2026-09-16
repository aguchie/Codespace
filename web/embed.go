// Package web provides embedded static assets for the Codespace gateway.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

// GetStaticFileSystem returns an http.FileSystem for the embedded static files.
func GetStaticFileSystem() http.FileSystem {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

// ReadStaticFile reads a file from the embedded static filesystem.
func ReadStaticFile(name string) ([]byte, error) {
	return staticFS.ReadFile("static/" + name)
}
