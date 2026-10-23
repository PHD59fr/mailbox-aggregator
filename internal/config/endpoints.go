package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (c Config) IMAPHostname() string {
	if c.IMAPProxyHostname != "" {
		return c.IMAPProxyHostname
	}
	return c.MailProxyHostname
}

func (c Config) SMTPHostname() string {
	if c.SMTPProxyHostname != "" {
		return c.SMTPProxyHostname
	}
	return c.MailProxyHostname
}

// MailProxyHostnames lists the identities required by the shared TLS certificate.
func (c Config) MailProxyHostnames() []string {
	var candidates []string
	if c.IMAPProxyEnabled {
		candidates = append(candidates, c.IMAPHostname())
	}
	if c.SMTPProxyEnabled {
		candidates = append(candidates, c.SMTPHostname())
	}
	if !c.IMAPProxyEnabled && !c.SMTPProxyEnabled {
		candidates = append(candidates, c.IMAPHostname(), c.SMTPHostname())
	}
	var result []string
	seen := make(map[string]bool)
	for _, host := range candidates {
		key := strings.ToLower(host)
		if host != "" && !seen[key] {
			result = append(result, host)
			seen[key] = true
		}
	}
	return result
}

func ValidateServerHostname(host string) error {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\\\r\n\x00 \t") {
		return errors.New("provide a DNS hostname or server IP address")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid server hostname")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return errors.New("invalid server hostname")
			}
		}
	}
	return nil
}

func publicURL(value, environment string) (string, error) {
	u, err := url.Parse(value)
	if err != nil {
		return "", errors.New("APP_PUBLIC_URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || ValidateServerHostname(u.Hostname()) != nil {
		return "", errors.New("APP_PUBLIC_URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("APP_PUBLIC_URL has an invalid port")
		}
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if environment == "production" || (!strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback())) {
			return "", errors.New("APP_PUBLIC_URL requires HTTPS for remote or production access")
		}
	}
	u.Path = ""
	u.RawPath = ""
	return u.String(), nil
}
