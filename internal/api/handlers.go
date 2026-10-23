package api

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	stdmail "net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/vault"
)

func (h *handlers) ready(c *gin.Context) {
	if !h.deps.Auth.Available() {
		respondError(c, http.StatusServiceUnavailable, "AUTH_PROVIDER_UNAVAILABLE", "RFC 9807 authentication is not configured.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (h *handlers) provider(c *gin.Context) (auth.FlowProvider, bool) {
	p, ok := h.deps.Auth.(auth.FlowProvider)
	return p, ok && h.deps.Auth.Available()
}
func (h *handlers) registerStart(c *gin.Context) {
	if h.registrationClosed(c) {
		return
	}
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		Login   string `json:"login"`
		Request string `json:"registration_request"`
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid registration request.")
		return
	}
	id, response, uid, err := p.RegistrationStart(in.Login, in.Request)
	if err != nil {
		if errors.Is(err, filestore.ErrConflict) {
			respondError(c, 409, "ACCOUNT_EXISTS", "An account with this login already exists.")
		} else {
			respondError(c, 400, "INVALID_INPUT", "Invalid registration request.")
		}
		return
	}
	c.JSON(200, gin.H{"flow_id": id, "registration_response": response, "user_id": uid})
}
func (h *handlers) registerFinish(c *gin.Context) {
	if h.registrationClosed(c) {
		return
	}
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		FlowID string `json:"flow_id"`
		auth.RegistrationPayload
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid registration completion.")
		return
	}
	result, err := p.RegistrationFinish(in.FlowID, in.RegistrationPayload)
	if err != nil {
		if errors.Is(err, filestore.ErrConflict) {
			respondError(c, 409, "ACCOUNT_EXISTS", "An account with this login already exists.")
		} else {
			respondError(c, 400, "REGISTRATION_FAILED", "Registration could not be completed.")
		}
		return
	}
	setSessionCookie(c, result.Token, h.deps.Config.CookieSecure, int(h.deps.Config.SessionMaxLifetime.Seconds()))
	c.JSON(200, result.View)
}
func (h *handlers) loginStart(c *gin.Context) {
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		Login   string `json:"login"`
		Request string `json:"start_login_request"`
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid authentication request.")
		return
	}
	flowID, response, err := p.LoginStart(in.Login, in.Request, "", uuid.Nil)
	if err != nil {
		respondError(c, 401, "AUTHENTICATION_FAILED", "Authentication failed.")
		return
	}
	c.JSON(200, gin.H{"flow_id": flowID, "login_response": response})
}
func (h *handlers) loginFinish(c *gin.Context) {
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		FlowID  string `json:"flow_id"`
		Request string `json:"finish_login_request"`
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid authentication completion.")
		return
	}
	result, err := p.LoginFinish(in.FlowID, in.Request)
	if err != nil {
		respondError(c, 401, "AUTHENTICATION_FAILED", "Authentication failed.")
		return
	}
	setSessionCookie(c, result.Token, h.deps.Config.CookieSecure, int(h.deps.Config.SessionMaxLifetime.Seconds()))
	c.JSON(200, result.View)
}
func (h *handlers) reauthStart(c *gin.Context) {
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		Login   string `json:"login"`
		Request string `json:"start_login_request"`
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid reauthentication request.")
		return
	}
	view := mustSession(c)
	flowID, response, err := p.LoginStart(in.Login, in.Request, c.GetString(contextToken), view.UserID)
	if err != nil {
		respondError(c, 401, "AUTHENTICATION_FAILED", "Authentication failed.")
		return
	}
	c.JSON(200, gin.H{"flow_id": flowID, "login_response": response})
}
func (h *handlers) reauthFinish(c *gin.Context) {
	p, ok := h.provider(c)
	if !ok {
		h.authUnavailable(c)
		return
	}
	var in struct {
		FlowID  string `json:"flow_id"`
		Request string `json:"finish_login_request"`
	}
	if c.ShouldBindJSON(&in) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid reauthentication completion.")
		return
	}
	result, err := p.LoginFinish(in.FlowID, in.Request)
	if err != nil || result.Token != "" {
		respondError(c, 401, "AUTHENTICATION_FAILED", "Authentication failed.")
		return
	}
	c.JSON(200, result.View)
}
func (h *handlers) authUnavailable(c *gin.Context) {
	respondError(c, http.StatusServiceUnavailable, "AUTH_PROVIDER_UNAVAILABLE", "RFC 9807 authentication is unavailable; no insecure fallback is enabled.")
}
func (h *handlers) me(c *gin.Context) {
	view := mustSession(c)
	c.JSON(http.StatusOK, gin.H{"user_id": view.UserID, "state": view.State})
}
func (h *handlers) session(c *gin.Context) { c.JSON(http.StatusOK, mustSession(c)) }

func (h *handlers) logout(c *gin.Context) {
	token := c.GetString(contextToken)
	h.deps.Sessions.Revoke(token)
	h.deps.watches.stopSession(token)
	clearSessionCookie(c, h.deps.Config.CookieSecure)
	c.Status(http.StatusNoContent)
}
func (h *handlers) lock(c *gin.Context) {
	view, err := h.deps.Sessions.Lock(c.GetString(contextToken))
	if err != nil {
		respondError(c, http.StatusUnauthorized, "SESSION_EXPIRED", "The session is invalid or expired.")
		return
	}
	h.deps.watches.stopSession(c.GetString(contextToken))
	c.Header("X-Lock-Revision", strconv.FormatUint(view.LockRevision, 10))
	c.Status(http.StatusNoContent)
}

type credentialInput struct {
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Security    string `json:"security"`
	Username    string `json:"username"`
	SenderName  string `json:"sender_name,omitempty"`
	ReplyTo     string `json:"reply_to,omitempty"`
	SenderEmail string `json:"sender_email,omitempty"`
	Password    string `json:"password,omitempty"`
	OAuthToken  string `json:"oauth_token,omitempty"`
}
type activateInput struct {
	Mode            string                     `json:"mode"`
	LockRevision    *uint64                    `json:"lock_revision"`
	LifetimeSeconds int64                      `json:"lifetime_seconds"`
	Accounts        map[string]credentialInput `json:"accounts"`
	SMTP            map[string]credentialInput `json:"smtp"`
}

var errMailServerUnavailable = errors.New("Mail server could not be resolved. The server may be temporarily unavailable.")

func credentialError(c *gin.Context, err error) {
	if errors.Is(err, errMailServerUnavailable) {
		respondError(c, http.StatusServiceUnavailable, "MAIL_SERVER_UNAVAILABLE", err.Error())
		return
	}
	respondError(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
}

func (h *handlers) activateCredentials(c *gin.Context) {
	var input activateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The request exceeds the configured size limit.")
			return
		}
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Invalid credential activation request.")
		return
	}
	maxCred := h.deps.Config.MaxAccounts
	if maxCred <= 0 {
		maxCred = 20
	}
	if len(input.Accounts) > maxCred || len(input.SMTP) > maxCred {
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Too many credential records.")
		return
	}
	if input.Mode != "" && input.Mode != "merge" && input.Mode != "replace" {
		respondError(c, 400, "INVALID_INPUT", "Invalid activation mode.")
		return
	}
	if input.LockRevision == nil {
		respondError(c, http.StatusBadRequest, "LOCK_REVISION_REQUIRED", "Reload the application before activating mail credentials.")
		return
	}
	accountsResult, err := h.decodeCredentials(c, input.Accounts)
	if err != nil && len(accountsResult.Credentials) == 0 {
		credentialError(c, err)
		return
	}
	if len(accountsResult.Credentials) == 0 && len(accountsResult.Errors) > 0 {
		// Return the first error, preserving errMailServerUnavailable for DNS failures
		for _, e := range accountsResult.Errors {
			if strings.Contains(e, "could not be resolved") {
				credentialError(c, errMailServerUnavailable)
			} else {
				credentialError(c, errors.New(e))
			}
			return
		}
	}
	defer clearCredentialCopies(accountsResult.Credentials)
	smtpResult, err := h.decodeCredentials(c, input.SMTP)
	if err != nil && len(smtpResult.Credentials) == 0 {
		credentialError(c, err)
		return
	}
	if len(smtpResult.Credentials) == 0 && len(smtpResult.Errors) > 0 {
		for _, e := range smtpResult.Errors {
			if strings.Contains(e, "could not be resolved") {
				credentialError(c, errMailServerUnavailable)
			} else {
				credentialError(c, errors.New(e))
			}
			return
		}
	}
	defer clearCredentialCopies(smtpResult.Credentials)
	if c.Request.Context().Err() != nil {
		respondError(c, http.StatusRequestTimeout, "ACTIVATION_CANCELLED", "Credential activation was cancelled.")
		return
	}
	lifetime := time.Duration(input.LifetimeSeconds) * time.Second
	if lifetime == 0 {
		lifetime = 30 * time.Minute
	}
	var view session.View
	if input.Mode == "merge" {
		view, err = h.deps.Sessions.MergeCredentials(c.GetString(contextToken), accountsResult.Credentials, smtpResult.Credentials, lifetime, *input.LockRevision)
	} else if input.Accounts == nil && input.SMTP != nil {
		view, err = h.deps.Sessions.SetSMTPCredentials(c.GetString(contextToken), smtpResult.Credentials, lifetime, *input.LockRevision)
	} else if input.SMTP == nil && input.Accounts != nil {
		view, err = h.deps.Sessions.SetReceivingCredentials(c.GetString(contextToken), accountsResult.Credentials, lifetime, *input.LockRevision)
	} else {
		view, err = h.deps.Sessions.SetCredentials(c.GetString(contextToken), accountsResult.Credentials, smtpResult.Credentials, lifetime, *input.LockRevision)
	}
	if errors.Is(err, session.ErrCredentialLimit) {
		respondError(c, 400, "CREDENTIAL_LIMIT", "Too many active mail identities. Remove an identity or lock the vault before retrying.")
		return
	}
	if errors.Is(err, session.ErrLockRevision) {
		respondError(c, http.StatusConflict, "LOCK_REVISION_CONFLICT", "The vault was locked in this session. Unlock it again before activating mail credentials.")
		return
	}
	if errors.Is(err, session.ErrExpired) || errors.Is(err, session.ErrNotFound) {
		respondError(c, http.StatusUnauthorized, "SESSION_EXPIRED", "The session is invalid or expired.")
		return
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Credential activation was rejected.")
		return
	}
	type activationResponse struct {
		session.View
		AccountErrors map[string]string `json:"account_errors,omitempty"`
		SMTPErrors    map[string]string `json:"smtp_errors,omitempty"`
	}
	c.JSON(http.StatusOK, activationResponse{View: view, AccountErrors: accountsResult.Errors, SMTPErrors: smtpResult.Errors})
}

type credentialDecodeResult struct {
	Credentials map[string]session.Credential
	Errors      map[string]string
}

func (h *handlers) decodeCredentials(c *gin.Context, input map[string]credentialInput) (credentialDecodeResult, error) {
	result := credentialDecodeResult{Credentials: make(map[string]session.Credential), Errors: make(map[string]string)}
	for id, value := range input {
		if len(id) < 1 || len(id) > 128 || len(value.Username) > 512 || (value.Security != "tls" && value.Security != "starttls") {
			result.Errors[id] = "invalid credential identifier"
			continue
		}
		if _, err := h.deps.Outbound.ResolveFirst(c.Request.Context(), value.Host, value.Port); err != nil {
			if errors.Is(err, outbound.ErrPortNotAllowed) || errors.Is(err, outbound.ErrUnsafeDestination) {
				result.Errors[id] = "mail server destination is not permitted"
			} else {
				result.Errors[id] = errMailServerUnavailable.Error()
			}
			continue
		}
		if (value.Password == "") == (value.OAuthToken == "") {
			result.Errors[id] = "exactly one credential secret is required"
			continue
		}
		if value.SenderEmail != "" {
			address, err := stdmail.ParseAddress(value.SenderEmail)
			if err != nil || address.Address != value.SenderEmail || strings.ContainsAny(value.SenderName+value.SenderEmail, "\r\n\x00") || len(value.SenderName) > 128 || len(value.SenderEmail) > 254 {
				result.Errors[id] = "invalid sending identity"
				continue
			}
		}
		if value.ReplyTo != "" {
			address, err := stdmail.ParseAddress(value.ReplyTo)
			if err != nil || address.Address != value.ReplyTo || len(value.ReplyTo) > 254 || strings.ContainsAny(value.ReplyTo, "\r\n\x00") {
				result.Errors[id] = "invalid reply address"
				continue
			}
		}
		result.Credentials[id] = session.Credential{Host: value.Host, Port: value.Port, Security: value.Security, Username: value.Username, SenderName: value.SenderName, SenderEmail: value.SenderEmail, ReplyTo: value.ReplyTo, Secret: []byte(value.Password), OAuthToken: []byte(value.OAuthToken)}
	}
	return result, nil
}

func (h *handlers) vaultMetadata(c *gin.Context) { h.sendUserFile(c, "vault.json", true) }
func (h *handlers) vaultFile(c *gin.Context)     { h.sendUserFile(c, c.Param("name")+".enc", false) }

// Keep the persistence revision independent of transport ETags, which a
// compression proxy may weaken or replace. If-Match still uses the exact
// stored-file revision and rejects concurrent changes.
func setResourceRevision(c *gin.Context, revision string) {
	c.Header("ETag", revision)
	c.Header("X-Resource-Revision", revision)
	c.Header("Cache-Control", "no-store, no-transform")
}

func (h *handlers) sendUserFile(c *gin.Context, name string, metadata bool) {
	view := mustSession(c)
	data, etag, err := h.deps.Store.ReadUserFile(view.UserID, name)
	if err != nil {
		fileError(c, err)
		return
	}
	if metadata {
		if err = vault.ValidateMetadataJSON(data); err != nil {
			respondError(c, http.StatusInternalServerError, "VAULT_CORRUPT", "Vault metadata is invalid.")
			return
		}
	} else if err = vault.ValidateEncryptedFileJSON(data); err != nil {
		respondError(c, http.StatusInternalServerError, "VAULT_CORRUPT", "Encrypted vault file is invalid.")
		return
	}
	setResourceRevision(c, etag)
	c.Data(http.StatusOK, "application/json", data)
}
func (h *handlers) putVaultFile(c *gin.Context) {
	name := c.Param("name") + ".enc"
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The request exceeds the configured size limit.")
			return
		}
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Unable to read request.")
		return
	}
	if err = vault.ValidateEncryptedFileJSON(data); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ENCRYPTED_FILE", "The encrypted file envelope is invalid.")
		return
	}
	expected := strings.TrimSpace(c.GetHeader("If-Match"))
	if expected == "" {
		respondError(c, http.StatusPreconditionRequired, "REVISION_REQUIRED", "If-Match is required.")
		return
	}
	view := mustSession(c)
	revision, err := h.deps.Store.WriteUserFile(view.UserID, name, data, expected, view.AuthRevision)
	if err != nil {
		fileError(c, err)
		return
	}
	if name == "accounts.enc" {
		h.deps.watches.stopSession(c.GetString(contextToken))
	}
	if (name == "accounts.enc" || name == "smtp.enc") && h.deps.Proxy != nil {
		h.deps.Proxy.Invalidate(view.UserID)
	}
	setResourceRevision(c, revision)
	c.Status(http.StatusNoContent)
}

func (h *handlers) deleteMe(c *gin.Context) {
	token := c.GetString(contextToken)
	if !h.deps.Sessions.IsFresh(token, h.deps.Config.ReauthLifetime) {
		respondError(c, http.StatusUnauthorized, "REAUTHENTICATION_REQUIRED", "Fresh authentication is required.")
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Confirmation != "DELETE" {
		respondError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "Type DELETE to confirm permanent deletion.")
		return
	}
	id := mustSession(c).UserID
	if err := h.deps.Users.DeleteOwn(id); err != nil {
		if errors.Is(err, filestore.ErrConflict) {
			respondError(c, http.StatusConflict, "DELETION_IN_PROGRESS", "Account deletion is already in progress.")
			return
		}
		respondError(c, http.StatusInternalServerError, "DELETION_FAILED", "Local account deletion did not complete.")
		return
	}
	clearSessionCookie(c, h.deps.Config.CookieSecure)
	c.Status(http.StatusNoContent)
}

func fileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, filestore.ErrInvalidName):
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Vault file not found.")
	case errors.Is(err, filestore.ErrConflict):
		respondError(c, http.StatusConflict, "REVISION_CONFLICT", "The file changed since it was read.")
	case errors.Is(err, filestore.ErrTooLarge):
		respondError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The file exceeds the configured size limit.")
	case errors.Is(err, os.ErrNotExist):
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Vault file not found.")
	default:
		respondError(c, http.StatusInternalServerError, "STORAGE_ERROR", "The vault operation failed.")
	}
}
func mustSession(c *gin.Context) session.View {
	value, _ := c.Get(contextSession)
	return value.(session.View)
}
func clearSessionCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", secure, true)
}
func setSessionCookie(c *gin.Context, token string, secure bool, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, token, maxAge, "/", "", secure, true)
}
func decodeBase64(value string, max int) ([]byte, error) {
	if len(value) > max*2 {
		return nil, errors.New("encoded value too large")
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func (h *handlers) registrationClosed(c *gin.Context) bool {
	if !h.deps.Config.RegistrationDisabled {
		return false
	}
	respondError(c, 403, "REGISTRATION_DISABLED", "Registration is closed on this instance.")
	return true
}
func (h *handlers) authOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"registration_enabled": !h.deps.Config.RegistrationDisabled})
}
