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
		router.Group("/assets", func(c *gin.Context) {
			// Vite gives each asset a content hash. Only these versioned files
			// may remain cached across deployments.
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Next()
		}).StaticFS("", http.FS(assetFS))
	}
	router.NoRoute(func(c *gin.Context) {
		// Never cache the app shell, unversioned files or missing chunks.
		c.Header("Cache-Control", "no-store")
		if c.Request.Method != http.MethodGet || strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/assets" || strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Status(http.StatusNotFound)
			return
		}
		requested := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
		if requested != "." && requested != "" && requested != "index.html" {
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
