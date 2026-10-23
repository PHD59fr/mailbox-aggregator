package mailproxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/config"
)

func LoadTLSConfig(cfg config.Config) (*tls.Config, error) {
	pair, err := tls.LoadX509KeyPair(cfg.MailProxyCertFile, cfg.MailProxyKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load mail proxy TLS certificate/key: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if err = leaf.VerifyHostname(cfg.MailProxyHostname); err != nil {
		return nil, errors.New("mail proxy certificate does not match MAIL_PROXY_HOSTNAME")
	}
	if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return nil, errors.New("mail proxy certificate is outside its validity period")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}, nil
}

type CertificateInfo struct {
	Created     bool
	SelfSigned  bool
	Fingerprint string
	Expires     time.Time
}

// EnsureCertificate preserves existing key pairs and creates a local certificate
// only when neither file exists. No CA registration or remote request occurs.
func EnsureCertificate(host, certFile, keyFile string) (CertificateInfo, error) {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\\\r\n\x00 \t") {
		return CertificateInfo{}, errors.New("provide a DNS hostname or server IP address")
	}
	if ip := net.ParseIP(host); ip == nil {
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return CertificateInfo{}, errors.New("invalid proxy hostname")
			}
			for _, r := range label {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
					return CertificateInfo{}, errors.New("invalid proxy hostname")
				}
			}
		}
	}
	if certFile == "" || keyFile == "" || filepath.Clean(certFile) == filepath.Clean(keyFile) {
		return CertificateInfo{}, errors.New("use distinct certificate and private-key paths")
	}
	certMissing, keyMissing := false, false
	for _, entry := range []struct {
		path    string
		missing *bool
	}{{certFile, &certMissing}, {keyFile, &keyMissing}} {
		_, err := os.Lstat(entry.path)
		if errors.Is(err, os.ErrNotExist) {
			*entry.missing = true
		} else if err != nil {
			return CertificateInfo{}, err
		}
	}
	if certMissing != keyMissing {
		return CertificateInfo{}, errors.New("incomplete TLS key pair; restore the missing file instead of overwriting the existing certificate or key")
	}
	if certMissing {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return CertificateInfo{}, err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return CertificateInfo{}, err
		}
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: time.Now().Add(-5 * time.Minute), NotAfter: time.Now().Add(397 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = []net.IP{ip}
		} else {
			template.DNSNames = []string{host}
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			return CertificateInfo{}, err
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return CertificateInfo{}, err
		}
		defer clear(keyDER)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		defer clear(keyPEM)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		for _, dir := range []string{filepath.Dir(certFile), filepath.Dir(keyFile)} {
			if err = os.MkdirAll(dir, 0700); err != nil {
				return CertificateInfo{}, fmt.Errorf("create certificate directory (must be writable by the container user): %w", err)
			}
		}
		if err = writeNewKeyFile(keyFile, keyPEM); err != nil {
			return CertificateInfo{}, err
		}
		if err = writeNewKeyFile(certFile, certPEM); err != nil {
			_ = os.Remove(keyFile)
			return CertificateInfo{}, err
		}
	}
	tlsConfig, err := LoadTLSConfig(config.Config{MailProxyHostname: host, MailProxyCertFile: certFile, MailProxyKeyFile: keyFile})
	if err != nil {
		return CertificateInfo{}, err
	}
	leaf, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		return CertificateInfo{}, err
	}
	fingerprint := sha256.Sum256(leaf.Raw)
	return CertificateInfo{Created: certMissing, SelfSigned: leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil, Fingerprint: hex.EncodeToString(fingerprint[:]), Expires: leaf.NotAfter}, nil
}

func writeNewKeyFile(name string, data []byte) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
