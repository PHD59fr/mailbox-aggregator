package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/mailproxy"
	"github.com/gin-gonic/gin"
)

func (h *handlers) proxySettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.deps.Proxy == nil {
		setResourceRevision(c, "*")
		c.JSON(200, mailproxy.Status{IMAPPort: 993, SMTPPort: 465, WebURL: h.deps.Config.PublicURL})
		return
	}
	status, revision, err := h.deps.Proxy.Status(mustSession(c).UserID)
	if err != nil {
		fileError(c, err)
		return
	}
	setResourceRevision(c, revision)
	c.JSON(200, status)
}

func (h *handlers) saveProxySettings(c *gin.Context) {
	if h.deps.Proxy == nil || (!h.deps.Config.IMAPProxyEnabled && !h.deps.Config.SMTPProxyEnabled) {
		respondError(c, 409, "PROXY_UNAVAILABLE", "Mail proxies are disabled on this server.")
		return
	}
	var input struct {
		Password string `json:"password"`
		mailproxy.Targets
	}
	if c.ShouldBindJSON(&input) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid proxy settings.")
		return
	}
	maxSenders := h.deps.Config.MaxAccounts
	if maxSenders <= 0 {
		maxSenders = 20
	}
	if len(input.Senders) > maxSenders {
		respondError(c, 400, "INVALID_INPUT", "Too many sender addresses.")
		return
	}
	if (input.IMAP != nil && input.AccountsRevision == "") || ((input.SMTP != nil || len(input.Senders) > 0) && input.SMTPRevision == "") {
		respondError(c, 428, "REVISION_REQUIRED", "Reload provider identities before configuring client access.")
		return
	}
	targets := []*mailproxy.Target{input.IMAP, input.SMTP}
	for i := range input.Senders {
		targets = append(targets, &input.Senders[i])
	}
	for _, target := range targets {
		if target == nil {
			continue
		}
		if target.ID == "" || target.Username == "" || len(target.Username) > 512 || len(target.Password) < 1 || len(target.Password) > 4096 || strings.ContainsAny(target.Username+target.Password, "\x00\r\n") || (target.Security != "tls" && target.Security != "starttls") {
			respondError(c, 400, "INVALID_INPUT", "Choose a saved identity with a valid password and TLS configuration.")
			return
		}
		if _, err := h.deps.Outbound.ResolveFirst(c.Request.Context(), target.Host, target.Port); err != nil {
			credentialError(c, err)
			return
		}
	}
	h.writeProxy(c, input.Password, input.Targets)
}

func (h *handlers) disableProxy(c *gin.Context) {
	if h.deps.Proxy == nil {
		c.Status(http.StatusNoContent)
		return
	}
	h.writeProxy(c, "", mailproxy.Targets{})
}

func (h *handlers) writeProxy(c *gin.Context, password string, targets mailproxy.Targets) {
	c.Header("Cache-Control", "no-store")
	expected := strings.TrimSpace(c.GetHeader("If-Match"))
	if expected == "" {
		respondError(c, 428, "REVISION_REQUIRED", "Reload proxy settings before saving.")
		return
	}
	revision, err := h.deps.Proxy.Save(mustSession(c).UserID, password, targets, expected, mustSession(c).AuthRevision)
	if err != nil {
		if errors.Is(err, filestore.ErrConflict) {
			fileError(c, err)
			return
		}
		respondError(c, 400, "PROXY_SAVE_FAILED", "Could not save proxy settings. Use a dedicated password of at least 16 characters and available identities.")
		return
	}
	setResourceRevision(c, revision)
	c.Status(http.StatusNoContent)
}
