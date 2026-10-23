package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCertificateSetupDoesNotRequireAuthenticationOrStartTheServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAIL_PROXY_HOSTNAME", "192.0.2.10")
	t.Setenv("MAIL_PROXY_TLS_CERT", filepath.Join(dir, "fullchain.pem"))
	t.Setenv("MAIL_PROXY_TLS_KEY", filepath.Join(dir, "privkey.pem"))
	t.Setenv("OPAQUE_SERVER_SETUP", "")
	var output bytes.Buffer
	if err := ensureMailProxyCertificate(nil, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "self-signed") || !strings.Contains(output.String(), "SHA-256 fingerprint") {
		t.Fatal("client trust instructions missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); !os.IsNotExist(err) {
		t.Fatal("certificate setup created application data")
	}
}

func TestCertificateSetupUsesSeparateHostnamesAndEnabledProtocols(t *testing.T) {
	for _, smtpEnabled := range []string{"true", "false"} {
		t.Run(smtpEnabled, func(t *testing.T) {
			dir := t.TempDir()
			certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
			t.Setenv("MAIL_PROXY_HOSTNAME", "")
			t.Setenv("IMAP_PROXY_HOSTNAME", "imap.example.test")
			t.Setenv("SMTP_PROXY_HOSTNAME", "smtp.example.test")
			t.Setenv("IMAP_PROXY_ENABLED", "true")
			t.Setenv("SMTP_PROXY_ENABLED", smtpEnabled)
			t.Setenv("MAIL_PROXY_TLS_CERT", certFile)
			t.Setenv("MAIL_PROXY_TLS_KEY", keyFile)
			var output bytes.Buffer
			if err := ensureMailProxyCertificate(nil, &output); err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := x509.ParseCertificate(pair.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := leaf.VerifyHostname("imap.example.test"); err != nil {
				t.Fatal(err)
			}
			if covered := leaf.VerifyHostname("smtp.example.test") == nil; covered != (smtpEnabled == "true") {
				t.Fatal("certificate identities do not match enabled protocols")
			}
		})
	}
}

func TestCertificateSetupWithoutHostnameSkipsGeneration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAIL_PROXY_HOSTNAME", "")
	t.Setenv("MAIL_PROXY_TLS_CERT", filepath.Join(dir, "fullchain.pem"))
	t.Setenv("MAIL_PROXY_TLS_KEY", filepath.Join(dir, "privkey.pem"))
	var output bytes.Buffer
	if err := ensureMailProxyCertificate(nil, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Mail proxies disabled: MAIL_PROXY_HOSTNAME is not specified.") {
		t.Fatal("missing disabled explanation")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("TLS files created without a server identity")
	}
}
