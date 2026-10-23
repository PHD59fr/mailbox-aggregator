package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestSeparatePublicEndpointsAndEnvironmentPrecedence(t *testing.T) {
	file := writeEnv(t, t.TempDir(), "APP_PUBLIC_URL=https://old.example.test\nMAIL_PROXY_HOSTNAME=mail.example.test\nIMAP_PROXY_HOSTNAME=old-imap.example.test\nIMAP_PROXY_ENABLED=true\nSMTP_PROXY_ENABLED=true\n")
	cfg, err := load([]string{"APP_PUBLIC_URL=https://webmail.example.test/", "IMAP_PROXY_HOSTNAME=imap.example.test", "SMTP_PROXY_HOSTNAME=smtp.example.test"}, file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://webmail.example.test" || cfg.IMAPHostname() != "imap.example.test" || cfg.SMTPHostname() != "smtp.example.test" || cfg.ListenAddress != ":8080" {
		t.Fatal("public endpoints were not applied independently of the listen address")
	}
	if !reflect.DeepEqual(cfg.MailProxyHostnames(), []string{"imap.example.test", "smtp.example.test"}) {
		t.Fatal("certificate identities omit an enabled protocol")
	}
	cfg, err = load(nil, file)
	if err != nil || cfg.IMAPHostname() != "old-imap.example.test" || cfg.SMTPHostname() != "mail.example.test" {
		t.Fatalf("shared hostname fallback failed: %v", err)
	}
}

func TestIndependentProxyHostnameActivation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		values     []string
		imap, smtp bool
		hosts      []string
	}{
		{"separate", []string{"IMAP_PROXY_HOSTNAME=imap.example.test", "SMTP_PROXY_HOSTNAME=smtp.example.test"}, true, true, []string{"imap.example.test", "smtp.example.test"}},
		{"imap only", []string{"IMAP_PROXY_HOSTNAME=imap.example.test"}, true, false, []string{"imap.example.test"}},
		{"smtp only", []string{"SMTP_PROXY_HOSTNAME=smtp.example.test"}, false, true, []string{"smtp.example.test"}},
		{"shared", []string{"MAIL_PROXY_HOSTNAME=mail.example.test"}, true, true, []string{"mail.example.test"}},
		{"disabled SMTP excluded", []string{"IMAP_PROXY_HOSTNAME=imap.example.test", "SMTP_PROXY_HOSTNAME=bad:465", "SMTP_PROXY_ENABLED=false"}, true, false, []string{"imap.example.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := load(append([]string{"IMAP_PROXY_ENABLED=true", "SMTP_PROXY_ENABLED=true"}, tc.values...))
			if err != nil || cfg.IMAPProxyEnabled != tc.imap || cfg.SMTPProxyEnabled != tc.smtp || !reflect.DeepEqual(cfg.MailProxyHostnames(), tc.hosts) {
				t.Fatalf("unexpected proxy activation: %+v %v", cfg, err)
			}
			if tc.imap != tc.smtp && tc.name != "disabled SMTP excluded" && cfg.MailProxyDisabledReason == "" {
				t.Fatal("missing explanation for unavailable protocol")
			}
		})
	}
	for _, host := range []string{"smtp://smtp.example.test", "smtp.example.test:465", "*.example.test", "bad_host", "-bad.example.test"} {
		if _, err := load([]string{"SMTP_PROXY_ENABLED=true", "SMTP_PROXY_HOSTNAME=" + host}); err == nil {
			t.Fatalf("invalid public SMTP identity accepted: %q", host)
		}
	}
}

func TestPublicWebURLValidation(t *testing.T) {
	for _, value := range []string{"", "https://webmail.example.test", "https://webmail.example.test:8443/", "http://localhost:8080", "http://127.0.0.1:8080", "https://[2001:db8::1]"} {
		cfg, err := load([]string{"APP_PUBLIC_URL=" + value})
		if err != nil || cfg.PublicURL != strings.TrimSuffix(value, "/") {
			t.Fatalf("valid URL rejected: %q %v", value, err)
		}
	}
	for _, value := range []string{"webmail.example.test", "javascript:alert(1)", "ftp://webmail.example.test", "https://user:private-password@example.test", "https://example.test/path", "https://example.test/?query=secret", "https://example.test/#fragment", "https://example.test:99999", "http://example.test", "https://*.example.test"} {
		_, err := load([]string{"APP_PUBLIC_URL=" + value})
		if err == nil || strings.Contains(err.Error(), "private-password") {
			t.Fatalf("invalid URL accepted or credentials disclosed: %q", value)
		}
	}
	if _, err := load([]string{"APP_ENV=production", "SESSION_COOKIE_SECURE=true", "APP_PUBLIC_URL=http://localhost:8080"}); err == nil {
		t.Fatal("production HTTP public URL accepted")
	}
}
