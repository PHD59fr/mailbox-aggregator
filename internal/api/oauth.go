package api

import (
	stdmail "net/mail"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *handlers) oauthUnlocked(c *gin.Context) bool {
	if _, err := h.deps.Sessions.Peek(c.GetString(contextToken)); err != nil {
		respondError(c, 423, "VAULT_LOCKED", "Unlock your vault before connecting or refreshing an OAuth account.")
		return false
	}
	return true
}
func (h *handlers) oauthOptions(c *gin.Context) {
	if !h.oauthUnlocked(c) {
		return
	}
	c.JSON(200, h.deps.OAuth.Options())
}
func (h *handlers) oauthStart(c *gin.Context) {
	if !h.oauthUnlocked(c) {
		return
	}
	var input struct {
		Contacts     bool    `json:"contacts"`
		Provider     string  `json:"provider"`
		Email        string  `json:"email"`
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision || len(input.Email) > 254 || strings.ContainsAny(input.Email, "\r\n\x00") {
		respondError(c, 400, "INVALID_INPUT", "Choose a provider and mailbox email address.")
		return
	}
	email, err := stdmail.ParseAddress(input.Email)
	if err != nil || email.Address != input.Email {
		respondError(c, 400, "INVALID_INPUT", "Enter a mailbox email address without a display name.")
		return
	}
	authorization, err := h.deps.OAuth.BeginWithContacts(c.GetString(contextToken), input.Provider, input.Email, *input.LockRevision, input.Contacts)
	if err != nil {
		respondError(c, 422, "OAUTH_UNAVAILABLE", "OAuth is not configured for this provider, or the vault session expired.")
		return
	}
	c.JSON(200, gin.H{"authorization_url": authorization})
}
func (h *handlers) oauthCallback(c *gin.Context) {
	state := c.Query("state")
	if len(state) > 128 || h.deps.OAuth.Complete(c.Request.Context(), c.Param("provider"), state, c.Query("code"), c.Query("error") != "") != nil {
		respondError(c, 400, "OAUTH_FLOW_EXPIRED", "Authorization expired or the vault was locked. Return to Settings and reconnect.")
		return
	}
	// This public callback cannot rely on a SameSite=Strict session cookie.
	// A subsequent authenticated POST takes the result, bound to the original
	// unlocked session and its lock revision. Tokens never enter this URL.
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(303, strings.TrimRight(h.deps.Config.PublicURL, "/")+"/settings?oauth_result="+url.QueryEscape(state))
}
func (h *handlers) oauthResult(c *gin.Context) {
	if !h.oauthUnlocked(c) {
		return
	}
	var input struct {
		State string `json:"state"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.State) > 128 {
		respondError(c, 400, "INVALID_INPUT", "Invalid authorization state.")
		return
	}
	result, err := h.deps.OAuth.Take(c.GetString(contextToken), input.State)
	if err != nil {
		respondError(c, 422, "OAUTH_AUTHORIZATION_FAILED", "Authorization failed, expired or was already used. Reconnect from Settings.")
		return
	}
	c.JSON(200, result)
}
func (h *handlers) oauthRefresh(c *gin.Context) {
	if !h.oauthUnlocked(c) {
		return
	}
	var input struct {
		Provider     string  `json:"provider"`
		RefreshToken string  `json:"refresh_token"`
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 400, "INVALID_INPUT", "Invalid OAuth refresh request.")
		return
	}
	result, err := h.deps.OAuth.Refresh(c.Request.Context(), c.GetString(contextToken), input.Provider, input.RefreshToken, *input.LockRevision)
	if err != nil {
		respondError(c, 422, "OAUTH_REFRESH_FAILED", "OAuth refresh failed. Reconnect the account if authorization was revoked or expired.")
		return
	}
	c.JSON(200, result)
}
func (h *handlers) oauthRevoke(c *gin.Context) {
	if !h.oauthUnlocked(c) {
		return
	}
	var input struct {
		Provider     string  `json:"provider"`
		RefreshToken string  `json:"refresh_token"`
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 400, "INVALID_INPUT", "Invalid revocation request.")
		return
	}
	if err := h.deps.OAuth.Revoke(c.Request.Context(), c.GetString(contextToken), input.Provider, input.RefreshToken, *input.LockRevision); err != nil {
		respondError(c, 422, "OAUTH_REVOCATION_FAILED", "Revocation could not be confirmed. Check the provider's application permissions.")
		return
	}
	c.Status(204)
}

func (h *handlers) googleContacts(c *gin.Context) {
	if !h.deps.Config.GoogleContactsEnabled {
		respondError(c, 404, "CONNECTOR_DISABLED", "Google contacts are disabled by the administrator.")
		return
	}
	var input struct {
		AccessToken  string  `json:"access_token"`
		Page         string  `json:"page"`
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 400, "INVALID_INPUT", "Invalid contact request.")
		return
	}
	result, err := h.deps.OAuth.Contacts(c.Request.Context(), c.GetString(contextToken), input.AccessToken, input.Page, *input.LockRevision)
	if err != nil {
		respondError(c, 422, "GOOGLE_CONTACTS_FAILED", "Google contacts could not be read. Enable People API for the application and reconnect with contacts.readonly consent.")
		return
	}
	c.JSON(200, result)
}
