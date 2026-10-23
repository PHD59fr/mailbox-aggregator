package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RegistrationDisabled    bool
	Environment             string
	ListenAddress           string
	DataDirectory           string
	LogLevel                slog.Level
	CookieSecure            bool
	TrustedProxies          []string
	SessionIdleTimeout      time.Duration
	SessionMaxLifetime      time.Duration
	ReauthLifetime          time.Duration
	RequestTimeout          time.Duration
	MaxRequestBytes         int64
	MaxVaultFileBytes       int64
	MaxAccounts             int
	AllowPrivateMail        bool
	AllowedMailCIDRs        []netip.Prefix
	AllowedMailPorts        map[uint16]struct{}
	OPAQUEServerSetup       string
	IMAPProxyEnabled        bool
	SMTPProxyEnabled        bool
	IMAPProxyAddress        string
	SMTPProxyAddress        string
	MailProxyHostname       string
	MailProxyCertFile       string
	MailProxyKeyFile        string
	MailProxyDisabledReason string
}

func Load() (Config, error) {
	filenames := []string{".env"}
	if executable, err := os.Executable(); err == nil {
		filenames = append(filenames, filepath.Join(filepath.Dir(executable), ".env"))
	}
	return load(os.Environ(), filenames...)
}

func load(process []string, filenames ...string) (Config, error) {
	values, directory, err := readEnvironment(process, filenames...)
	if err != nil {
		return Config{}, err
	}
	env := values.string
	envBool := values.boolean
	c := Config{
		Environment:        env("APP_ENV", "development"),
		ListenAddress:      env("APP_LISTEN_ADDR", ":8080"),
		DataDirectory:      env("APP_DATA_DIR", "./data"),
		CookieSecure:       envBool("SESSION_COOKIE_SECURE", false),
		SessionIdleTimeout: 30 * time.Minute,
		SessionMaxLifetime: 12 * time.Hour,
		ReauthLifetime:     5 * time.Minute,
		RequestTimeout:     30 * time.Second,
		MaxRequestBytes:    2 << 20,
		MaxVaultFileBytes:  1 << 20,
		MaxAccounts:        20,
		AllowPrivateMail:   envBool("ALLOW_PRIVATE_MAIL_SERVERS", false),
		OPAQUEServerSetup:  strings.TrimSpace(values["OPAQUE_SERVER_SETUP"]),
		AllowedMailPorts:   map[uint16]struct{}{143: {}, 465: {}, 587: {}, 993: {}},
		IMAPProxyAddress:   env("IMAP_PROXY_LISTEN_ADDR", ":1993"),
		SMTPProxyAddress:   env("SMTP_PROXY_LISTEN_ADDR", ":1465"),
		MailProxyHostname:  env("MAIL_PROXY_HOSTNAME", ""),
		MailProxyCertFile:  env("MAIL_PROXY_TLS_CERT", ""),
		MailProxyKeyFile:   env("MAIL_PROXY_TLS_KEY", ""),
	}
	for name, target := range map[string]*bool{"REGISTRATION_DISABLED": &c.RegistrationDisabled, "IMAP_PROXY_ENABLED": &c.IMAPProxyEnabled, "SMTP_PROXY_ENABLED": &c.SMTPProxyEnabled} {
		if raw := strings.TrimSpace(values[name]); raw != "" {
			value, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				return Config{}, fmt.Errorf("invalid %s", name)
			}
			*target = value
		}
	}
	if (c.IMAPProxyEnabled || c.SMTPProxyEnabled) && c.MailProxyHostname == "" {
		c.IMAPProxyEnabled = false
		c.SMTPProxyEnabled = false
		c.MailProxyDisabledReason = "Mail proxies disabled: MAIL_PROXY_HOSTNAME is not specified."
	}
	if c.IMAPProxyEnabled || c.SMTPProxyEnabled {
		if (c.MailProxyCertFile == "") != (c.MailProxyKeyFile == "") {
			return Config{}, errors.New("provide both MAIL_PROXY_TLS_CERT and MAIL_PROXY_TLS_KEY, or leave both empty for automatic certificates")
		}
		if strings.ContainsAny(c.MailProxyHostname, "/\\\r\n\x00 \t") || (strings.Contains(c.MailProxyHostname, ":") && net.ParseIP(c.MailProxyHostname) == nil) {
			return Config{}, errors.New("MAIL_PROXY_HOSTNAME must be a hostname")
		}
	}
	for _, path := range []*string{&c.MailProxyCertFile, &c.MailProxyKeyFile} {
		if *path != "" && directory != "" && !filepath.IsAbs(*path) {
			*path = filepath.Join(directory, *path)
		}
	}
	if directory != "" && !filepath.IsAbs(c.DataDirectory) {
		explicitDataDirectory := false
		for _, assignment := range process {
			if key, value, ok := strings.Cut(assignment, "="); ok && key == "APP_DATA_DIR" && strings.TrimSpace(value) != "" {
				explicitDataDirectory = true
			}
		}
		if !explicitDataDirectory {
			c.DataDirectory = filepath.Join(directory, c.DataDirectory)
		}
	}
	if (c.IMAPProxyEnabled || c.SMTPProxyEnabled) && c.MailProxyCertFile == "" && c.MailProxyKeyFile == "" {
		c.MailProxyCertFile = filepath.Join(c.DataDirectory, "proxy-certs", "fullchain.pem")
		c.MailProxyKeyFile = filepath.Join(c.DataDirectory, "proxy-certs", "privkey.pem")
	}
	if c.SessionIdleTimeout, err = values.duration("SESSION_IDLE_TIMEOUT", c.SessionIdleTimeout); err != nil {
		return Config{}, err
	}
	if c.SessionMaxLifetime, err = values.duration("SESSION_MAX_LIFETIME", c.SessionMaxLifetime); err != nil {
		return Config{}, err
	}
	if c.ReauthLifetime, err = values.duration("REAUTH_LIFETIME", c.ReauthLifetime); err != nil {
		return Config{}, err
	}
	if c.RequestTimeout, err = values.duration("REQUEST_TIMEOUT", c.RequestTimeout); err != nil {
		return Config{}, err
	}
	if c.MaxRequestBytes, err = values.int64("MAX_REQUEST_BYTES", c.MaxRequestBytes); err != nil {
		return Config{}, err
	}
	if c.MaxVaultFileBytes, err = values.int64("MAX_VAULT_FILE_BYTES", c.MaxVaultFileBytes); err != nil {
		return Config{}, err
	}
	if c.MaxAccounts, err = values.int("MAX_ACCOUNTS", c.MaxAccounts); err != nil {
		return Config{}, err
	}
	if c.MaxAccounts <= 0 || c.MaxAccounts > 100 {
		c.MaxAccounts = 20
	}
	switch strings.ToLower(env("LOG_LEVEL", "info")) {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}
	if raw := strings.TrimSpace(values["TRUSTED_PROXIES"]); raw != "" {
		c.TrustedProxies = split(raw)
	}
	for _, raw := range split(values["ALLOWED_MAIL_CIDRS"]) {
		prefix, parseErr := netip.ParsePrefix(raw)
		if parseErr != nil {
			return Config{}, fmt.Errorf("invalid ALLOWED_MAIL_CIDRS entry %q: %w", raw, parseErr)
		}
		c.AllowedMailCIDRs = append(c.AllowedMailCIDRs, prefix.Masked())
	}
	if raw := strings.TrimSpace(values["ALLOWED_MAIL_PORTS"]); raw != "" {
		c.AllowedMailPorts = make(map[uint16]struct{})
		for _, item := range split(raw) {
			parsed, parseErr := strconv.ParseUint(item, 10, 16)
			if parseErr != nil || parsed == 0 {
				return Config{}, fmt.Errorf("invalid ALLOWED_MAIL_PORTS entry %q", item)
			}
			c.AllowedMailPorts[uint16(parsed)] = struct{}{}
		}
	}
	if c.SessionIdleTimeout <= 0 || c.SessionMaxLifetime <= 0 || c.SessionIdleTimeout > c.SessionMaxLifetime {
		return Config{}, errors.New("invalid session timeout policy")
	}
	if c.MaxRequestBytes < 1024 || c.MaxVaultFileBytes < 1024 {
		return Config{}, errors.New("size limits must be at least 1024 bytes")
	}
	if c.Environment == "production" && !c.CookieSecure {
		return Config{}, errors.New("SESSION_COOKIE_SECURE must be true in production")
	}
	return c, nil
}

func (e environment) string(name, fallback string) string {
	if value := strings.TrimSpace(e[name]); value != "" {
		return value
	}
	return fallback
}
func (e environment) boolean(name string, fallback bool) bool {
	value := strings.TrimSpace(e[name])
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func (e environment) duration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(e[name])
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}
func (e environment) int64(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(e[name])
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}
func (e environment) int(name string, fallback int) (int, error) {
	value := strings.TrimSpace(e[name])
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}
func split(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
