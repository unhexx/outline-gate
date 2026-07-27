package webui

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticRoot embed.FS

// StaticFS is the UI asset filesystem rooted at static/.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticRoot, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
