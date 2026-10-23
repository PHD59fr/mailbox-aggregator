package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mailproxy"
	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/example/mail-aggregator/internal/webui"
)

type Dependencies struct {
	Config     config.Config
	Logger     *slog.Logger
	Store      *filestore.Store
	Index      *filestore.UserIndex
	Users      *user.Service
	Sessions   *session.Manager
	Auth       auth.Availability
	Outbound   outbound.Policy
	Sender     MessageSender
	Operations *operations.Registry
	Receiver   MailWatcher
	Reader     MailReader
	Tester     ConnectionTester
	Proxy      *mailproxy.Service
	watches    *watchRegistry
}

type MessageSender interface {
	Send(context.Context, smtp.Configuration, smtp.BuiltMessage) error
}

func NewRouter(deps Dependencies) (*gin.Engine, error) {
	if deps.Tester == nil {
		deps.Tester = providerTester{policy: deps.Outbound}
	}
	deps.Sessions.SetAccountLimit(deps.Config.MaxAccounts)
	deps.watches = newWatchRegistry(deps.Config.MaxAccounts)
	if deps.Receiver == nil {
		deps.Receiver = imap.Receiver{Policy: deps.Outbound}
	}
	if deps.Reader == nil {
		deps.Reader = imap.Receiver{Policy: deps.Outbound}
	}
	if deps.Sender == nil {
		deps.Sender = smtp.Sender{Policy: deps.Outbound, OperationTimeout: deps.Config.RequestTimeout}
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(deps.Config.TrustedProxies); err != nil {
		return nil, err
	}

	// Global base limiter (generous, mainly for static assets and health)
	globalLimiter := newRateLimiter(600, 60*time.Second)
	// Auth endpoints: stricter (registration/login attempts)
	authLimiter := newRateLimiter(30, 60*time.Second)
	// Mail operations: moderate (folder sync, message reads, sends)
	mailLimiter := newRateLimiter(300, 60*time.Second)

	router.Use(recovery(deps.Logger), requestLogger(deps.Logger), securityHeaders(), bodyLimit(deps.Config.MaxRequestBytes), requestTimeout(deps.Config.RequestTimeout), globalLimiter.middleware())
	h := &handlers{deps: deps}
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", h.ready)
	v1 := router.Group("/api/v1")
	v1.GET("/auth/options", h.authOptions)
	{
		authGroup := v1.Group("/auth")
		authGroup.Use(authLimiter.middleware())
		authGroup.POST("/register/start", h.registerStart)
		authGroup.POST("/register/finish", h.registerFinish)
		authGroup.POST("/login/start", h.loginStart)
		authGroup.POST("/login/finish", h.loginFinish)
	}
	protected := v1.Group("")
	protected.Use(authenticate(deps.Sessions, deps.Users.IsActive, deps.Store), requireCSRF(deps.Sessions), mailLimiter.middleware())
	protected.GET("/me", h.me)
	protected.DELETE("/me", h.deleteMe)
	protected.POST("/auth/logout", h.logout)
	protected.POST("/auth/reauth/start", h.reauthStart)
	protected.POST("/auth/reauth/finish", h.reauthFinish)
	protected.POST("/auth/password/start", h.passwordStart)
	protected.POST("/auth/password/finish", h.passwordFinish)
	protected.GET("/session", h.session)
	protected.POST("/session/credentials", h.activateCredentials)
	protected.DELETE("/session/credentials", h.lock)
	protected.POST("/messages/send", h.sendMessage)
	protected.POST("/connections/test", newConnectionTestLimiter().middleware(), h.testConnection)
	protected.GET("/proxy", h.proxySettings)
	protected.PUT("/proxy", h.saveProxySettings)
	protected.DELETE("/proxy", h.disableProxy)
	protected.POST("/mailboxes/watch", h.watchMailboxes)
	protected.POST("/mailboxes/presence", h.mailPresence)
	protected.GET("/mailboxes/:account/messages", h.listMessages)
	protected.GET("/mailboxes/:account/folders", h.listFolders)
	protected.POST("/mailboxes/:account/folders", h.manageFolder)
	protected.POST("/mailboxes/:account/drafts", h.saveRemoteDraft)
	protected.DELETE("/mailboxes/:account/drafts/:uid", h.deleteRemoteDraft)
	protected.POST("/mailboxes/:account/messages/:uid/flag", h.messageFlag)
	protected.POST("/mailboxes/:account/messages/:uid/transfer", h.messageTransfer)
	protected.GET("/mailboxes/:account/messages/:uid", h.readMessage)
	protected.GET("/mailboxes/:account/messages/:uid/document", h.messageDocument)
	protected.GET("/mailboxes/:account/messages/:uid/source", h.messageSource)
	protected.POST("/mailboxes/:account/messages/:uid/seen", h.markMessageSeen)
	protected.POST("/mailboxes/:account/messages/:uid/unseen", h.markMessageSeen)
	protected.POST("/mailboxes/:account/messages/:uid/trash", h.trashMessage)
	protected.POST("/mailboxes/:account/messages/:uid/redirect", h.redirectMessage)
	protected.GET("/mailboxes/:account/messages/:uid/attachments/:part", h.downloadAttachment)
	protected.POST("/vault/import", h.importConfiguration)
	protected.GET("/vault/metadata", h.vaultMetadata)
	protected.GET("/vault/files/:name", h.vaultFile)
	protected.PUT("/vault/files/:name", h.putVaultFile)
	if err := webui.Register(router); err != nil {
		return nil, err
	}
	return router, nil
}

type handlers struct{ deps Dependencies }
