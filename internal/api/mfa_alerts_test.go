package api

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMFAFailureAlertThresholdAndNoSecrets(t *testing.T) {
	var logs bytes.Buffer
	router := gin.New()
	router.POST("/mfa", mfaFailureAlerts(slog.New(slog.NewJSONHandler(&logs, nil))), func(c *gin.Context) { c.Status(401) })
	for i := 0; i < 9; i++ {
		req := httptest.NewRequest("POST", "/mfa", strings.NewReader(`{"flow_id":"secret-flow","code":"secret-code"}`))
		req.RemoteAddr = "192.0.2.1:1234"
		router.ServeHTTP(httptest.NewRecorder(), req)
		if i < 4 && logs.Len() != 0 {
			t.Fatal("alert before threshold")
		}
	}
	if strings.Count(logs.String(), "mfa_failed_attempts") != 1 || strings.Contains(logs.String(), "secret-") {
		t.Fatalf("missing, repeated or sensitive alert: %s", logs.String())
	}
}
