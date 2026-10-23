package api

import (
	"context"
	"errors"
	"github.com/example/mail-aggregator/internal/sieve"
	"github.com/gin-gonic/gin"
	"time"
)

func (h *handlers) sieveRequest(c *gin.Context) {
	if !h.deps.Config.SieveEnabled {
		respondError(c, 404, "CONNECTOR_DISABLED", "ManageSieve is disabled by the administrator.")
		return
	}
	var input struct {
		sieve.Request
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 400, "INVALID_INPUT", "Invalid Sieve settings or vault session.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	id, ok := h.deps.watches.start(c.GetString(contextToken), mustSession(c).UserID, 1, cancel)
	if !ok {
		respondError(c, 429, "WATCH_LIMIT", "Too many active provider connections.")
		return
	}
	defer h.deps.watches.remove(id)
	if h.deps.Operations != nil {
		defer h.deps.Operations.Track(mustSession(c).UserID, cancel)()
	}
	if latest, err := h.deps.Sessions.Peek(c.GetString(contextToken)); err != nil || latest.LockRevision != *input.LockRevision {
		respondError(c, 423, "VAULT_LOCKED", "The vault session changed. Unlock it and try again.")
		return
	}
	result, err := (sieve.Client{Policy: h.deps.Outbound}).Do(ctx, input.Request)
	if err != nil {
		status, code, message := 502, "SIEVE_FAILED", "ManageSieve request failed. Check settings, TLS, allowed ports and server capabilities."
		if errors.Is(err, sieve.ErrConflict) {
			status, code, message = 409, "SIEVE_CONFLICT", err.Error()
		}
		if errors.Is(err, sieve.ErrSyntax) {
			status, code, message = 422, "SIEVE_SYNTAX", err.Error()
		}
		respondError(c, status, code, message)
		return
	}
	if latest, err := h.deps.Sessions.Peek(c.GetString(contextToken)); err != nil || latest.LockRevision != *input.LockRevision {
		respondError(c, 423, "VAULT_LOCKED", "The vault session changed. Check the remote result before retrying any change.")
		return
	}
	c.JSON(200, result)
}
