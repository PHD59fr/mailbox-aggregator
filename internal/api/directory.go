package api

import (
	"context"
	"github.com/example/mail-aggregator/internal/directory"
	"github.com/gin-gonic/gin"
	"time"
)

func (h *handlers) connectorOptions(c *gin.Context) {
	c.JSON(200, gin.H{"google_contacts": h.deps.Config.GoogleContactsEnabled, "carddav": h.deps.Config.CardDAVEnabled, "ldap": h.deps.Config.LDAPEnabled, "sieve": h.deps.Config.SieveEnabled})
}
func (h *handlers) readDirectory(c *gin.Context) {
	var input struct {
		directory.Config
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 400, "INVALID_INPUT", "Invalid directory settings or vault session.")
		return
	}
	if !(input.Kind == "carddav" && h.deps.Config.CardDAVEnabled || input.Kind == "ldap" && h.deps.Config.LDAPEnabled) {
		respondError(c, 404, "CONNECTOR_DISABLED", "This contact connector is disabled by the administrator.")
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
	result, err := (directory.Client{Policy: h.deps.Outbound}).Read(ctx, input.Config)
	if err != nil {
		respondError(c, 502, "DIRECTORY_FAILED", "Could not read contacts. Check the address, TLS, credentials, permissions and allowed ports.")
		return
	}
	if latest, err := h.deps.Sessions.Peek(c.GetString(contextToken)); err != nil || latest.LockRevision != *input.LockRevision {
		respondError(c, 423, "VAULT_LOCKED", "The vault session changed. Unlock it and try again.")
		return
	}
	c.JSON(200, result)
}
