package mailproxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCertificateCreatesPersistentDNSAndIPCertificates(t *testing.T) {
	for _, host := range []string{"mail.example.test", "192.0.2.10", "2001:db8::10"} {
		t.Run(host, func(t *testing.T) {
			dir := t.TempDir()
			certFile, keyFile := filepath.Join(dir, "certs", "fullchain.pem"), filepath.Join(dir, "certs", "privkey.pem")
			info, err := EnsureCertificate(host, certFile, keyFile)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Created || !info.SelfSigned || len(info.Fingerprint) != 64 {
				t.Fatal("unexpected certificate information")
			}
			cert, err := os.ReadFile(certFile)
			if err != nil {
				t.Fatal(err)
			}
			key, err := os.ReadFile(keyFile)
			if err != nil {
				t.Fatal(err)
			}
			block, _ := pem.Decode(cert)
			leaf, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if err = leaf.VerifyHostname(host); err != nil {
				t.Fatal(err)
			}
			if _, err = tls.X509KeyPair(cert, key); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{certFile, keyFile} {
				stat, err := os.Stat(name)
				if err != nil || stat.Mode().Perm() != 0600 {
					t.Fatal("unsafe TLS file permissions")
				}
			}
			again, err := EnsureCertificate(host, certFile, keyFile)
			if err != nil || again.Created || again.Fingerprint != info.Fingerprint {
				t.Fatal("rerun replaced a certificate")
			}
			certAfter, _ := os.ReadFile(certFile)
			keyAfter, _ := os.ReadFile(keyFile)
			if !bytes.Equal(cert, certAfter) || !bytes.Equal(key, keyAfter) {
				t.Fatal("existing key pair changed")
			}
		})
	}
}

func TestEnsureCertificateNeverOverwritesIncompleteOrMismatchedPairs(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(keyFile, []byte("existing-key-must-stay"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureCertificate("mail.example.test", certFile, keyFile); err == nil {
		t.Fatal("partial key pair overwritten")
	}
	key, _ := os.ReadFile(keyFile)
	if string(key) != "existing-key-must-stay" {
		t.Fatal("existing key modified")
	}
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureCertificate("mail.example.test", certFile, keyFile); err != nil {
		t.Fatal(err)
	}
	cert, _ := os.ReadFile(certFile)
	if _, err := EnsureCertificate("different.example.test", certFile, keyFile); err == nil {
		t.Fatal("mismatched certificate accepted")
	}
	after, _ := os.ReadFile(certFile)
	if !bytes.Equal(cert, after) {
		t.Fatal("certificate silently regenerated")
	}
}

func TestEnsureCertificateRejectsInvalidNamesWithoutWritingFiles(t *testing.T) {
	for _, host := range []string{"", "https://mail.example.test", "mail.example.test:465", "-mail.example.test", "mail/name", "mail\nexample.test", "*.example.test"} {
		dir := t.TempDir()
		certFile, keyFile := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
		if _, err := EnsureCertificate(host, certFile, keyFile); err == nil {
			t.Fatal("invalid server identity accepted")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatal("invalid input wrote TLS material")
		}
	}
}
