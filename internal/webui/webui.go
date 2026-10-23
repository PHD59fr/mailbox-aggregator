package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var assets embed.FS

func Register(router *gin.Engine) error {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return err
	}
	files := http.FS(dist)
	if assetFS, subErr := fs.Sub(dist, "assets"); subErr == nil {
		router.StaticFS("/assets", http.FS(assetFS))
	}
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}
		requested := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
		if requested != "." && requested != "" {
			if file, openErr := dist.Open(requested); openErr == nil {
				_ = file.Close()
				// The preview has an opaque sandbox origin. Only this public,
				// static stylesheet may be loaded from that isolated document.
				if requested == "compose-preview.css" {
					c.Header("Cross-Origin-Resource-Policy", "cross-origin")
				}
				c.FileFromFS(requested, files)
				return
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
	return nil
}
