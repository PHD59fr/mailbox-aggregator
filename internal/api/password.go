package api

import (
	"errors"
	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/gin-gonic/gin"
)

func (h *handlers) passwordStart(c *gin.Context) {
	provider, ok := h.deps.Auth.(auth.PasswordChanger)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var input struct {
		Login   string `json:"login"`
		Request string `json:"registration_request"`
		Rotate  bool   `json:"rotate_vmk"`
	}
	if c.ShouldBindJSON(&input) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid password change request.")
		return
	}
	id, response, err := provider.PasswordStart(input.Login, input.Request, c.GetString(contextToken), mustSession(c).UserID, input.Rotate)
	if err != nil {
		respondError(c, 403, "REAUTHENTICATION_REQUIRED", "Authenticate again with your current password before changing credentials.")
		return
	}
	c.JSON(200, gin.H{"flow_id": id, "registration_response": response})
}
func (h *handlers) passwordFinish(c *gin.Context) {
	provider, ok := h.deps.Auth.(auth.PasswordChanger)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var input struct {
		FlowID string `json:"flow_id"`
		auth.RegistrationPayload
	}
	if c.ShouldBindJSON(&input) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid credential update.")
		return
	}
	if err := provider.PasswordFinish(input.FlowID, c.GetString(contextToken), input.RegistrationPayload); err != nil {
		if errors.Is(err, filestore.ErrConflict) {
			fileError(c, err)
			return
		}
		respondError(c, 400, "CREDENTIAL_CHANGE_FAILED", "Credential change was not confirmed. Sign in again and check which password is active before retrying.")
		return
	}
	h.invalidateImportedSessions(c)
	c.Status(204)
}
