package api

import (
	"encoding/json"
	"errors"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/vault"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Import replaces only settings; the server never receives their plaintext or
// the export password/VMK. All three revision preconditions form one commit.
func (h *handlers) importConfiguration(c *gin.Context) {
	var input struct {
		Files map[string]filestore.ImportFile `json:"files"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(c, 413, "PAYLOAD_TOO_LARGE", "The import exceeds the request limit.")
			return
		}
		respondError(c, 400, "INVALID_INPUT", "Invalid encrypted configuration import.")
		return
	}
	if len(input.Files) != 3 {
		respondError(c, 400, "INVALID_INPUT", "Import requires accounts, smtp and preferences.")
		return
	}
	files := make(map[string]filestore.ImportFile)
	for _, name := range []string{"accounts", "smtp", "preferences"} {
		file, ok := input.Files[name]
		if !ok || vault.ValidateEncryptedFileJSON(file.Data) != nil || !json.Valid(file.Data) {
			respondError(c, 400, "INVALID_ENCRYPTED_FILE", "Invalid import envelope.")
			return
		}
		if file.Revision == "" {
			respondError(c, 428, "REVISION_REQUIRED", "Each imported collection requires its current revision.")
			return
		}
		files[name+".enc"] = file
	}
	if err := h.deps.Store.WriteUserFiles(mustSession(c).UserID, files, mustSession(c).AuthRevision); err != nil {
		if errors.Is(err, filestore.ErrConflict) || errors.Is(err, filestore.ErrTooLarge) {
			fileError(c, err)
			return
		}
		h.invalidateImportedSessions(c)
		// A commit may already be durable. Reloading reconciles it safely.
		respondError(c, http.StatusInternalServerError, "IMPORT_UNCONFIRMED", "The import could not be confirmed. Reload settings before trying again.")
		return
	}
	h.invalidateImportedSessions(c)
	c.Status(http.StatusNoContent)
}
func (h *handlers) invalidateImportedSessions(c *gin.Context) {
	id := mustSession(c).UserID
	h.deps.Sessions.RevokeUser(id)
	h.deps.watches.stopUser(id)
	if h.deps.Operations != nil {
		h.deps.Operations.CancelUser(id)
	}
	if h.deps.Proxy != nil {
		h.deps.Proxy.Invalidate(id)
	}
	clearSessionCookie(c, h.deps.Config.CookieSecure)
}
