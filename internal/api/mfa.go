package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *handlers) requireMFA() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.deps.MFA == nil {
			c.Next()
			return
		}
		view := mustSession(c)
		binding, err := h.deps.MFA.Binding(view.UserID)
		if err != nil {
			abort(c, 503, "MFA_UNAVAILABLE", "Second factor state is unavailable.")
			return
		}
		latest, err := h.deps.Sessions.Peek(c.GetString(contextToken))
		if err != nil {
			abort(c, 401, "SESSION_EXPIRED", "Sign in again.")
			return
		}
		view = latest
		c.Set(contextSession, view)
		if binding != "" && view.MFARevision != binding {
			h.deps.Sessions.Revoke(c.GetString(contextToken))
			abort(c, 401, "MFA_REQUIRED", "Sign in again with your second factor.")
			return
		}
		c.Next()
	}
}
func (h *handlers) verifyMFA(c *gin.Context) {
	var input struct {
		FlowID string `json:"flow_id"`
		Code   string `json:"code"`
	}
	if h.deps.MFA == nil || c.ShouldBindJSON(&input) != nil || len(input.FlowID) > 128 || len(input.Code) > 128 {
		respondError(c, 401, "MFA_FAILED", "The code or authentication flow is invalid or expired.")
		return
	}
	token, view, err := h.deps.MFA.VerifyLogin(input.FlowID, input.Code)
	if err != nil {
		respondError(c, 401, "MFA_FAILED", "The code or authentication flow is invalid or expired.")
		return
	}
	if !h.deps.Users.IsActive(view.UserID) {
		h.deps.Sessions.Revoke(token)
		respondError(c, 401, "MFA_FAILED", "Authentication failed.")
		return
	}
	_, revision, err := h.deps.Store.ReadUserFile(view.UserID, "auth.json")
	if err != nil || view.AuthRevision != "" && view.AuthRevision != revision {
		h.deps.Sessions.Revoke(token)
		respondError(c, 401, "MFA_FAILED", "Authentication changed. Sign in again.")
		return
	}
	setSessionCookie(c, token, h.deps.Config.CookieSecure, int(h.deps.Config.SessionMaxLifetime.Seconds()))
	c.JSON(200, view)
}
func (h *handlers) mfaStatus(c *gin.Context) {
	if h.deps.MFA == nil {
		c.JSON(200, gin.H{"available": false, "enabled": false})
		return
	}
	enabled, remaining, _, err := h.deps.MFA.Status(mustSession(c).UserID)
	if err != nil {
		respondError(c, 503, "MFA_UNAVAILABLE", "Second factor state is unavailable.")
		return
	}
	c.JSON(200, gin.H{"available": true, "enabled": enabled, "recovery_codes_remaining": remaining})
}
func (h *handlers) freshMFA(c *gin.Context) bool {
	if h.deps.MFA == nil {
		respondError(c, 503, "MFA_UNAVAILABLE", "Second factor support is unavailable.")
		return false
	}
	view := mustSession(c)

	provider, ok := h.deps.Auth.(interface{ ConsumeReauthentication(string, uuid.UUID) error })
	if !ok || provider.ConsumeReauthentication(c.GetString(contextToken), view.UserID) != nil {
		respondError(c, 403, "REAUTHENTICATION_REQUIRED", "Authenticate again with your main password before changing second factor settings.")
		return false
	}
	return true
}
func (h *handlers) enrollMFA(c *gin.Context) {
	var input struct {
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision {
		respondError(c, 409, "LOCK_REVISION_CONFLICT", "Unlock your vault and retry enrollment.")
		return
	}
	if !h.freshMFA(c) {
		return
	}
	secret, uri, err := h.deps.MFA.StartEnrollment(mustSession(c).UserID, c.GetString(contextToken), *input.LockRevision)
	if err != nil {
		respondError(c, 400, "MFA_ENROLLMENT_FAILED", "Could not start second factor enrollment.")
		return
	}
	c.JSON(200, gin.H{"secret": secret, "uri": uri, "expires_in": 300})
}
func (h *handlers) confirmMFA(c *gin.Context) {
	var input struct {
		Code string `json:"code"`
	}
	if h.deps.MFA == nil || c.ShouldBindJSON(&input) != nil || len(input.Code) > 128 {
		respondError(c, 400, "MFA_FAILED", "Enter a valid authenticator code.")
		return
	}

	codes, err := h.deps.MFA.ConfirmEnrollment(mustSession(c).UserID, c.GetString(contextToken), input.Code)
	if err != nil {
		respondError(c, 400, "MFA_FAILED", "The code is invalid or enrollment expired. Start enrollment again if needed.")
		return
	}
	h.deps.watches.stopUser(mustSession(c).UserID)
	if h.deps.Operations != nil {
		h.deps.Operations.CancelUser(mustSession(c).UserID)
	}
	c.JSON(200, gin.H{"recovery_codes": codes})
}
func (h *handlers) changeMFA(c *gin.Context, disable bool) {
	var input struct {
		Code         string  `json:"code"`
		LockRevision *uint64 `json:"lock_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.LockRevision == nil || *input.LockRevision != mustSession(c).LockRevision || len(input.Code) > 128 {
		respondError(c, 400, "MFA_FAILED", "Enter an authenticator or recovery code.")
		return
	}
	if !h.freshMFA(c) {
		return
	}
	codes, err := h.deps.MFA.Change(mustSession(c).UserID, c.GetString(contextToken), input.Code, disable, *input.LockRevision)
	if err != nil {
		respondError(c, 400, "MFA_FAILED", "The second factor code is invalid or was already used.")
		return
	}
	h.deps.watches.stopUser(mustSession(c).UserID)
	if h.deps.Operations != nil {
		h.deps.Operations.CancelUser(mustSession(c).UserID)
	}
	c.JSON(200, gin.H{"enabled": !disable, "recovery_codes": codes})
}
func (h *handlers) regenerateMFA(c *gin.Context) { h.changeMFA(c, false) }
func (h *handlers) disableMFA(c *gin.Context)    { h.changeMFA(c, true) }
