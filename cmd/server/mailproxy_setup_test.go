package main

import (
	"bytes"
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
