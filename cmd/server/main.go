package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/mail-aggregator/internal/diagnostic"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/mail-aggregator/internal/api"
	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/mailproxy"
	"github.com/example/mail-aggregator/internal/oauth"
	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/user"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--ensure-mail-proxy-certificate" {
		if err := ensureMailProxyCertificate(os.Args[2:], os.Stdout); err != nil {
			slog.Error("mail proxy certificate setup failed", "error", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-data" {
		report, err := diagnostic.Check(cfg.DataDirectory, cfg.OPAQUEServerSetup, cfg.MaxVaultFileBytes)
		if err != nil {
			slog.Error("backup validation failed", "error", err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if err := healthcheck(cfg.ListenAddress); err != nil {
			slog.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	if cfg.PublicURL != "" {
		logger.Info("public web interface configured", "url", cfg.PublicURL)
	}
	if cfg.MailProxyDisabledReason != "" {
		logger.Warn(cfg.MailProxyDisabledReason)
	}
	store, err := filestore.New(cfg.DataDirectory, cfg.MaxVaultFileBytes)
	if err != nil {
		logger.Error("open data store", "error", err)
		os.Exit(1)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		logger.Error("open user index", "error", err)
		os.Exit(1)
	}
	sessions, err := session.NewManager(cfg.SessionIdleTimeout, cfg.SessionMaxLifetime)
	if err != nil {
		logger.Error("create session manager", "error", err)
		os.Exit(1)
	}
	operations := operations.NewRegistry()
	users := user.NewService(store, index, sessions, operations)
	if err = users.RecoverDeletions(); err != nil {
		logger.Error("recover pending account deletions", "error", err)
		os.Exit(1)
	}
	setup, err := auth.ResolveServerSetup(store, index, cfg.OPAQUEServerSetup)
	if err != nil {
		logger.Error("load persistent OPAQUE setup", "error", err)
		os.Exit(1)
	}
	logger.Info("OPAQUE server setup ready", "source", setup.Source, "path", setup.Path)
	authProvider, err := auth.NewService(setup.Value, index, store, users, sessions)
	if err != nil {
		logger.Error("initialize RFC 9807 authentication", "error", err)
		os.Exit(1)
	}
	defer authProvider.SecondFactor().Close()
	oauthFlows := oauth.New(oauth.Config{GoogleContacts: cfg.GoogleContactsEnabled, PublicURL: cfg.PublicURL, Google: oauth.ProviderConfig{ClientID: cfg.OAuthGoogleClientID, ClientSecret: cfg.OAuthGoogleClientSecret}, Microsoft: oauth.ProviderConfig{ClientID: cfg.OAuthMicrosoftClientID, ClientSecret: cfg.OAuthMicrosoftClientSecret}}, sessions, operations)
	defer oauthFlows.Close()
	outboundPolicy := outbound.Policy{AllowPrivate: cfg.AllowPrivateMail, AllowedCIDRs: cfg.AllowedMailCIDRs, AllowedPorts: cfg.AllowedMailPorts, Timeout: 15 * time.Second}
	mailProxy := mailproxy.New(cfg, store, index, users, outboundPolicy, operations, logger)
	router, err := api.NewRouter(api.Dependencies{Config: cfg, Logger: logger, Store: store, Index: index, Users: users, Sessions: sessions, Auth: authProvider, Outbound: outboundPolicy, Operations: operations, Proxy: mailProxy, OAuth: oauthFlows})
	if err != nil {
		logger.Error("create router", "error", err)
		os.Exit(1)
	}
	if err = mailProxy.Start(); err != nil {
		logger.Error("start mail proxies", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: cfg.ListenAddress, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: cfg.RequestTimeout + 5*time.Second, WriteTimeout: cfg.RequestTimeout + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go sessions.RunCleanup(shutdownContext)
	// Listen first so "server listening" is only logged after the socket is open.
	ln, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		logger.Error("failed to listen", "address", cfg.ListenAddress, "error", err)
		os.Exit(1)
	}
	logger.Info("server listening", "address", cfg.ListenAddress)
	go func() {
		if serveErr := server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("http server failed", "error", serveErr)
			stop()
		}
	}()
	<-shutdownContext.Done()
	mailProxy.Close()
	operations.Close()
	sessions.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
