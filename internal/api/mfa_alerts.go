package api

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Operational alert only: no mail credentials, challenge IDs, codes, account
// identifiers or request bodies are retained. HTTP logs already record the IP.
func mfaFailureAlerts(logger *slog.Logger) gin.HandlerFunc {
	type window struct {
		start time.Time
		count int
	}
	var mu sync.Mutex
	windows := map[string]window{}
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() != 401 || logger == nil {
			return
		}
		now, ip := time.Now(), c.ClientIP()
		mu.Lock()
		for key, value := range windows {
			if now.Sub(value.start) >= time.Minute {
				delete(windows, key)
			}
		}
		value, exists := windows[ip]
		if !exists && len(windows) >= 4096 {
			mu.Unlock()
			return
		}
		if !exists {
			value.start = now
		}
		value.count++
		windows[ip] = value
		alert := value.count == 5
		mu.Unlock()
		if alert {
			logger.Warn("repeated second factor failures", "event", "mfa_failed_attempts", "client_ip", ip, "failures", 5, "window_seconds", 60, "request_id", c.GetString("request_id"))
		}
	}
}
