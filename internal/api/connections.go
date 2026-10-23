package api

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/gin-gonic/gin"
)

type ConnectionTester interface {
	Test(context.Context, string, session.Credential) error
}
type providerTester struct {
	policy outbound.Policy
	logger *slog.Logger
}

func (t providerTester) Test(ctx context.Context, protocol string, cfg session.Credential) error {
	if protocol == "imap" {
		return (imap.Receiver{Policy: t.policy, Logger: t.logger}).Test(ctx, imap.Configuration{Host: cfg.Host, Port: cfg.Port, Security: cfg.Security, Username: cfg.Username, Password: cfg.Secret, OAuthToken: cfg.OAuthToken, SASLMechanism: cfg.SASLMechanism})
	}
	return (smtp.Sender{Policy: t.policy, OperationTimeout: 20 * time.Second, Logger: t.logger}).Test(ctx, smtp.Configuration{Host: cfg.Host, Port: cfg.Port, Security: cfg.Security, Username: cfg.Username, Password: cfg.Secret, OAuthToken: cfg.OAuthToken})
}

type connectionTestLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newConnectionTestLimiter() *connectionTestLimiter {
	return &connectionTestLimiter{last: make(map[string]time.Time), now: time.Now}
}
func (l *connectionTestLimiter) allow(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if last, ok := l.last[ip]; ok && now.Sub(last) < 30*time.Second {
		return 30*time.Second - now.Sub(last)
	}
	if len(l.last) >= 10000 {
		for key, last := range l.last {
			if now.Sub(last) >= 30*time.Second {
				delete(l.last, key)
			}
		}
		if len(l.last) >= 10000 {
			return 30 * time.Second
		}
	}
	l.last[ip] = now
	return 0
}
func (l *connectionTestLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if retry := l.allow(c.ClientIP()); retry > 0 {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			abort(c, 429, "CONNECTION_TEST_RATE_LIMIT", "Only one IMAP or SMTP connection test is allowed per IP every 30 seconds.")
			return
		}
		c.Next()
	}
}
func (h *handlers) testConnection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Protocol string `json:"protocol" binding:"required,oneof=imap smtp"`
		credentialInput
	}
	if c.ShouldBindJSON(&input) != nil || (input.Password == "" && input.OAuthToken == "") || (input.Protocol == "smtp" && input.SASLMechanism != "") || input.Username == "" || len(input.Password) > 4096 || len(input.OAuthToken) > 16384 {
		respondError(c, 400, "INVALID_INPUT", "Enter a host, username and password to test the connection.")
		return
	}
	records, err := h.decodeCredentials(c, map[string]credentialInput{"probe": input.credentialInput})
	if err != nil && len(records.Credentials) == 0 {
		respondError(c, 400, "INVALID_INPUT", "Invalid mail connection settings.")
		return
	}
	if len(records.Credentials) == 0 {
		respondError(c, 400, "INVALID_INPUT", "Invalid mail connection settings.")
		return
	}
	defer clearCredentialCopies(records.Credentials)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	id, ok := h.deps.watches.start(c.GetString(contextToken), mustSession(c).UserID, 1, cancel)
	if !ok {
		respondError(c, 429, "WATCH_LIMIT", "Too many active mail connections.")
		return
	}
	defer h.deps.watches.remove(id)
	if h.deps.Operations != nil {
		defer h.deps.Operations.Track(mustSession(c).UserID, cancel)()
	}
	if err := h.deps.Tester.Test(ctx, input.Protocol, records.Credentials["probe"]); err != nil {
		respondError(c, 502, "CONNECTION_TEST_FAILED", "Connection or authentication failed. Check the host, TLS mode and credentials.")
		return
	}
	c.JSON(200, gin.H{"status": "connected"})
}
