package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/maillog"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

const (
	contextSession = "authenticated-session"
	contextToken   = "session-token"
	sessionCookie  = "mail_session"
)

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		c.Next()
	}
}

func recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if value := recover(); value != nil {
				logger.Error("panic recovered", "request_id", c.GetString("request_id"), "panic_type", fmt.Sprintf("%T", value), "stack", string(debug.Stack()))
				abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred.")
			}
		}()
		c.Next()
	}
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := randomRequestID()
		c.Set("request_id", requestID)
		c.Request = c.Request.WithContext(maillog.WithRequestID(c.Request.Context(), requestID))
		c.Header("X-Request-ID", requestID)
		start := time.Now()
		c.Next()
		logger.Info("http request", "request_id", requestID, "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "latency_ms", time.Since(start).Milliseconds(), "client_ip", c.ClientIP())
	}
}

func bodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			limit := max
			// Only the authenticated send endpoint accepts larger, in-memory JSON
			// bodies to accommodate base64 attachments. Other routes keep their limit.
			if c.Request.Method == http.MethodPost && (c.FullPath() == "/api/v1/messages/send" || c.FullPath() == "/api/v1/mailboxes/:account/drafts") {
				limit = maxSendRequestBytes
			}
			if c.Request.Method == http.MethodPost && c.FullPath() == "/api/v1/vault/import" {
				limit = 4 * max
			}
			if c.Request.Method == http.MethodPost && c.FullPath() == "/api/v1/auth/password/finish" {
				limit = 8 * max
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func authenticate(manager *session.Manager, isActive func(uuid.UUID) bool, stores ...*filestore.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookie)
		if err != nil || token == "" {
			abort(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.")
			return
		}
		view, err := manager.Get(token)
		if err != nil {
			abort(c, http.StatusUnauthorized, "SESSION_EXPIRED", "The session is invalid or expired.")
			return
		}
		if !isActive(view.UserID) {
			manager.Revoke(token)
			abort(c, http.StatusForbidden, "ACCOUNT_UNAVAILABLE", "The account is unavailable.")
			return
		}
		if view.AuthRevision != "" && len(stores) > 0 {
			_, revision, err := stores[0].ReadUserFile(view.UserID, "auth.json")
			if err != nil || revision != view.AuthRevision {
				manager.Revoke(token)
				abort(c, 401, "SESSION_EXPIRED", "Authentication changed. Sign in again.")
				return
			}
		}
		c.Set(contextSession, view)
		c.Set(contextToken, token)
		c.Next()
	}
}

func requestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.FullPath() == "/api/v1/mailboxes/watch" {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func requireCSRF(manager *session.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		token := c.GetString(contextToken)
		supplied := strings.TrimSpace(c.GetHeader("X-CSRF-Token"))
		if supplied == "" || !manager.ValidateCSRF(token, supplied) {
			abort(c, http.StatusForbidden, "CSRF_INVALID", "The CSRF token is missing or invalid.")
			return
		}
		c.Next()
	}
}

type rateLimiter struct {
	mu           sync.Mutex
	entries      map[string]*rateEntry
	limit        int
	window       time.Duration
	maxEntries   int
	cleanupEvery int
	requests     int
}
type rateEntry struct {
	started time.Time
	count   int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit <= 0 {
		limit = 180
	}
	if window <= 0 {
		window = 60 * time.Second
	}
	maxEntries := 10000
	if limit*100 > maxEntries {
		maxEntries = limit * 100
	}
	return &rateLimiter{entries: make(map[string]*rateEntry), limit: limit, window: window, maxEntries: maxEntries, cleanupEvery: 1000}
}
func (r *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()
		r.mu.Lock()
		entry := r.entries[key]
		if entry == nil || now.Sub(entry.started) >= r.window {
			entry = &rateEntry{started: now}
			r.entries[key] = entry
		}
		entry.count++
		allowed := entry.count <= r.limit
		// Periodic cleanup of expired entries
		r.requests++
		if r.requests%r.cleanupEvery == 0 {
			r.cleanupLocked(now)
		}
		// Enforce max entries by evicting oldest
		if len(r.entries) > r.maxEntries {
			r.evictOldestLocked()
		}
		r.mu.Unlock()
		if !allowed {
			c.Header("Retry-After", "60")
			abort(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests.")
			return
		}
		c.Next()
	}
}
func (r *rateLimiter) cleanupLocked(now time.Time) {
	for k, e := range r.entries {
		if now.Sub(e.started) >= r.window {
			delete(r.entries, k)
		}
	}
}
func (r *rateLimiter) evictOldestLocked() {
	oldest := ""
	var oldestTime time.Time
	for k, e := range r.entries {
		if oldest == "" || e.started.Before(oldestTime) {
			oldest = k
			oldestTime = e.started
		}
	}
	if oldest != "" {
		delete(r.entries, oldest)
	}
}
func randomRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(value)
}
