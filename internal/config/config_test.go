package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeEnv(t *testing.T, directory, contents string) string {
	t.Helper()
	filename := filepath.Join(directory, ".env")
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestLoadConfigurationFromEnvFile(t *testing.T) {
	directory := t.TempDir()
	filename := writeEnv(t, directory, "OPAQUE_SERVER_SETUP=existing-stable-key\nAPP_DATA_DIR=./data\nAPP_LISTEN_ADDR=:8181\nSESSION_COOKIE_SECURE=true\nSESSION_IDLE_TIMEOUT=10m\nALLOWED_MAIL_PORTS=465,587\nTRUSTED_PROXIES=127.0.0.1/32\n")
	cfg, err := load(nil, filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OPAQUEServerSetup != "existing-stable-key" {
		t.Fatal("existing authentication setup was not loaded")
	}
	if cfg.DataDirectory != filepath.Join(directory, "data") || cfg.ListenAddress != ":8181" || !cfg.CookieSecure || cfg.SessionIdleTimeout != 10*time.Minute {
		t.Fatal("file settings were not applied")
	}
	if len(cfg.AllowedMailPorts) != 2 || len(cfg.TrustedProxies) != 1 {
		t.Fatal("collection settings were not loaded")
	}
}

func TestProcessEnvironmentTakesPrecedence(t *testing.T) {
	filename := writeEnv(t, t.TempDir(), "OPAQUE_SERVER_SETUP=file-key\nAPP_DATA_DIR=./data\nAPP_LISTEN_ADDR=:8181\nSESSION_COOKIE_SECURE=true\nSESSION_IDLE_TIMEOUT=10m\n")
	cfg, err := load([]string{"OPAQUE_SERVER_SETUP=environment-key", "APP_DATA_DIR=./custom", "APP_LISTEN_ADDR=:9191", "SESSION_COOKIE_SECURE=false", "SESSION_IDLE_TIMEOUT=15m"}, filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OPAQUEServerSetup != "environment-key" || cfg.DataDirectory != "./custom" || cfg.ListenAddress != ":9191" || cfg.CookieSecure || cfg.SessionIdleTimeout != 15*time.Minute {
		t.Fatal("explicit process configuration was overridden")
	}
	cfg, err = load([]string{"OPAQUE_SERVER_SETUP="}, filename)
	if err != nil || cfg.OPAQUEServerSetup != "" {
		t.Fatal("an explicit empty environment value should take precedence")
	}
}

func TestFallbackFileAndWorkingDirectoryPriority(t *testing.T) {
	working := t.TempDir()
	executable := t.TempDir()
	fallback := writeEnv(t, executable, "OPAQUE_SERVER_SETUP=binary-key\nAPP_DATA_DIR=./data\n")
	local := filepath.Join(working, ".env")
	cfg, err := load(nil, local, fallback)
	if err != nil || cfg.OPAQUEServerSetup != "binary-key" || cfg.DataDirectory != filepath.Join(executable, "data") {
		t.Fatalf("executable fallback was not applied: %v", err)
	}
	writeEnv(t, working, "OPAQUE_SERVER_SETUP=working-directory-key\n")
	cfg, err = load(nil, local, fallback)
	if err != nil || cfg.OPAQUEServerSetup != "working-directory-key" || cfg.DataDirectory != filepath.Join(working, "data") {
		t.Fatalf("working-directory file did not take precedence: %v", err)
	}
}

func TestLoadAutomaticallyReadsWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	writeEnv(t, directory, "OPAQUE_SERVER_SETUP=automatic-key\n")
	t.Chdir(directory)
	previous, existed := os.LookupEnv("OPAQUE_SERVER_SETUP")
	if err := os.Unsetenv("OPAQUE_SERVER_SETUP"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("OPAQUE_SERVER_SETUP", previous)
		} else {
			_ = os.Unsetenv("OPAQUE_SERVER_SETUP")
		}
	})
	cfg, err := Load()
	if err != nil || cfg.OPAQUEServerSetup != "automatic-key" {
		t.Fatalf("Load did not discover .env: %v", err)
	}
	if _, exists := os.LookupEnv("OPAQUE_SERVER_SETUP"); exists {
		t.Fatal("file loading unexpectedly modified the process environment")
	}
}

func TestEnvQuotesCommentsAndLiteralValues(t *testing.T) {
	text := "\ufeff# configuration\nexport OPAQUE_SERVER_SETUP='stable-key' # comment\nEMPTY=\nDOUBLE=\"quoted # value\\nnext\"\nLITERAL='$(echo never-executed)'\nHASH=abc#def\nCOMMENT=value # comment\n"
	values, err := parseEnvFile(bufio.NewScanner(strings.NewReader(text)))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"OPAQUE_SERVER_SETUP": "stable-key", "EMPTY": "", "DOUBLE": "quoted # value\nnext", "LITERAL": "$(echo never-executed)", "HASH": "abc#def", "COMMENT": "value"} {
		if values[key] != expected {
			t.Fatalf("unexpected parsing for %s", key)
		}
	}
}

func TestMalformedEnvDoesNotLeakSecretOrUseFallback(t *testing.T) {
	for _, contents := range []string{"OPAQUE_SERVER_SETUP='private-key-material\n", "BAD-KEY=private-key-material\n", "OPAQUE_SERVER_SETUP=\"private-key-material\" trailing\n"} {
		filename := writeEnv(t, t.TempDir(), contents)
		fallback := writeEnv(t, t.TempDir(), "OPAQUE_SERVER_SETUP=other-key\n")
		_, err := load(nil, filename, fallback)
		if err == nil || strings.Contains(err.Error(), "private-key-material") {
			t.Fatal("invalid configuration was accepted or its secret was exposed")
		}
	}
}

func TestMissingEnvFilePreservesDefaults(t *testing.T) {
	cfg, err := load(nil, filepath.Join(t.TempDir(), ".env"))
	if err != nil || cfg.OPAQUEServerSetup != "" || cfg.DataDirectory != "./data" || cfg.ListenAddress != ":8080" {
		t.Fatalf("unexpected configuration without .env: %v", err)
	}
}

func TestMailProxyConfigurationIsOptInAndRequiresTLS(t *testing.T) {
	cfg, err := load(nil)
	if err != nil || cfg.IMAPProxyEnabled || cfg.SMTPProxyEnabled || cfg.IMAPProxyAddress != ":1993" || cfg.SMTPProxyAddress != ":1465" {
		t.Fatal("proxy defaults unexpectedly expose listeners")
	}
	for _, values := range [][]string{{"IMAP_PROXY_ENABLED=true", "MAIL_PROXY_HOSTNAME=mail.example.test", "MAIL_PROXY_TLS_CERT=cert.pem"}, {"SMTP_PROXY_ENABLED=typo"}, {"SMTP_PROXY_ENABLED=true", "MAIL_PROXY_HOSTNAME=host:465", "MAIL_PROXY_TLS_CERT=cert.pem", "MAIL_PROXY_TLS_KEY=key.pem"}} {
		if _, err := load(values); err == nil {
			t.Fatal("invalid proxy configuration accepted")
		}
	}
	directory := t.TempDir()
	filename := writeEnv(t, directory, "IMAP_PROXY_ENABLED=true\nMAIL_PROXY_HOSTNAME=mail.example.test\nMAIL_PROXY_TLS_CERT=certs/fullchain.pem\nMAIL_PROXY_TLS_KEY=certs/privkey.pem\n")
	cfg, err = load(nil, filename)
	if err != nil || !cfg.IMAPProxyEnabled || cfg.SMTPProxyEnabled || cfg.MailProxyCertFile != filepath.Join(directory, "certs/fullchain.pem") {
		t.Fatalf("proxy config not loaded: %v", err)
	}
}

func TestProxyCertificatesDefaultToPersistentDataDirectory(t *testing.T) {
	dir := t.TempDir()
	file := writeEnv(t, dir, "IMAP_PROXY_ENABLED=true\nMAIL_PROXY_HOSTNAME=192.0.2.10\nMAIL_PROXY_TLS_CERT=\nMAIL_PROXY_TLS_KEY=\nAPP_DATA_DIR=./data\n")
	cfg, err := load(nil, file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MailProxyCertFile != filepath.Join(dir, "data/proxy-certs/fullchain.pem") || cfg.MailProxyKeyFile != filepath.Join(dir, "data/proxy-certs/privkey.pem") {
		t.Fatal("automatic TLS paths did not follow persistent data")
	}
	cfg, err = load([]string{"APP_DATA_DIR=/app/data"}, file)
	if err != nil || cfg.MailProxyCertFile != "/app/data/proxy-certs/fullchain.pem" {
		t.Fatal("container data directory was ignored")
	}
}

func TestMissingProxyHostnameDisablesGatewaysWithoutStoppingWebmail(t *testing.T) {
	for _, values := range [][]string{{"IMAP_PROXY_ENABLED=true"}, {"SMTP_PROXY_ENABLED=true"}, {"IMAP_PROXY_ENABLED=true", "SMTP_PROXY_ENABLED=true", "MAIL_PROXY_HOSTNAME=  ", "MAIL_PROXY_TLS_CERT=/missing/fullchain.pem", "MAIL_PROXY_TLS_KEY=/missing/privkey.pem"}} {
		cfg, err := load(values)
		if err != nil || cfg.IMAPProxyEnabled || cfg.SMTPProxyEnabled {
			t.Fatalf("missing proxy hostname blocked startup or left gateways enabled: %v", err)
		}
		if cfg.MailProxyDisabledReason != "Mail proxies disabled: MAIL_PROXY_HOSTNAME is not specified." {
			t.Fatal("missing clear disabled message")
		}
	}
	configured, err := load([]string{"IMAP_PROXY_ENABLED=true", "MAIL_PROXY_HOSTNAME=mail.example.test", "MAIL_PROXY_TLS_CERT=cert.pem", "MAIL_PROXY_TLS_KEY=key.pem"})
	if err != nil || !configured.IMAPProxyEnabled || configured.MailProxyDisabledReason != "" {
		t.Fatal("configured proxy incorrectly disabled")
	}
}

func TestRegistrationPolicyIsExplicitAndValidated(t *testing.T) {
	cfg, err := load([]string{"REGISTRATION_DISABLED=true"})
	if err != nil || !cfg.RegistrationDisabled {
		t.Fatal("closed registration not honored")
	}
	cfg, err = load(nil)
	if err != nil || cfg.RegistrationDisabled {
		t.Fatal("default registration policy changed")
	}
	if _, err := load([]string{"REGISTRATION_DISABLED=typo"}); err == nil {
		t.Fatal("invalid operator policy silently accepted")
	}
}
