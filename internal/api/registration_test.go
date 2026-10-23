package api

import (
	"github.com/example/mail-aggregator/internal/config"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestClosedRegistrationRejectsBothEndpointsBeforeProviderAndExposesPolicy(t *testing.T) {
	h := handlers{deps: Dependencies{Config: config.Config{RegistrationDisabled: true}}}
	router := gin.New()
	router.POST("/start", h.registerStart)
	router.POST("/finish", h.registerFinish)
	router.GET("/options", h.authOptions)
	for _, path := range []string{"/start", "/finish"} {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest("POST", path, nil))
		if r.Code != 403 {
			t.Fatalf("registration was not closed: %d", r.Code)
		}
	}
	r := httptest.NewRecorder()
	router.ServeHTTP(r, httptest.NewRequest("GET", "/options", nil))
	if r.Code != 200 || r.Body.String() != `{"registration_enabled":false}` {
		t.Fatal("public registration policy incorrect")
	}
}
