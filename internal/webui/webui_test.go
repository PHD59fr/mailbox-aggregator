package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStaticCachePolicyAndMissingChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := Register(router); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/contacts", "/compose", "/settings", "/index.html", "/theme.js", "/compose-preview.css", "/assets/missing-old-release.js", "/assets/missing.css"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unversioned content or missing asset can be cached")
			}
			if strings.HasPrefix(path, "/assets/") {
				if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "<html") {
					t.Fatal("missing chunk returned the application HTML")
				}
			} else if response.Code != http.StatusOK {
				t.Fatalf("public page or file failed: %d", response.Code)
			}
		})
	}
	entries, err := fs.ReadDir(assets, "dist/assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		for _, method := range []string{"GET", "HEAD"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, "/assets/"+entry.Name(), nil))
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatalf("hashed asset not served with immutable cache policy: %s %d", method, response.Code)
			}
		}
		return
	}
	t.Fatal("no embedded JavaScript asset found")
}
