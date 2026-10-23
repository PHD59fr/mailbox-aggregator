package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/gin-gonic/gin"
)

type unavailableResolver struct{}

func (unavailableResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, errors.New("private upstream DNS diagnostic")
}

func TestCredentialDNSFailureIsRetryableButUnsafeDestinationIsNot(t *testing.T) {
	h := handlers{deps: Dependencies{Outbound: outbound.Policy{Resolver: unavailableResolver{}, AllowedPorts: map[uint16]struct{}{993: {}}}}}
	for _, kind := range []string{"accounts", "smtp"} {
		for _, test := range []struct {
			host   string
			status int
			code   string
		}{{"imap.example.test", 503, "MAIL_SERVER_UNAVAILABLE"}, {"127.0.0.1", 400, "INVALID_INPUT"}} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/session/credentials", strings.NewReader(`{"lock_revision":0,"`+kind+`":{"own":{"host":"`+test.host+`","port":993,"security":"tls","username":"own","password":"secret"}}}`))
			c.Request.Header.Set("Content-Type", "application/json")
			h.activateCredentials(c)
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.code) || strings.Contains(recorder.Body.String(), "private upstream") {
				t.Fatalf("wrong activation error: %d %s", recorder.Code, recorder.Body.String())
			}
		}
	}
}
