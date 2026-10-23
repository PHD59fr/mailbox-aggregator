package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/mailproxy"
)

func ensureMailProxyCertificate(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("ensure-mail-proxy-certificate", flag.ContinueOnError)
	flags.SetOutput(output)
	host := flags.String("hostname", os.Getenv("MAIL_PROXY_HOSTNAME"), "DNS hostname or IP used by mail clients")
	imapHost := flags.String("imap-hostname", os.Getenv("IMAP_PROXY_HOSTNAME"), "IMAP DNS hostname or IP (overrides the shared hostname)")
	smtpHost := flags.String("smtp-hostname", os.Getenv("SMTP_PROXY_HOSTNAME"), "SMTP DNS hostname or IP (overrides the shared hostname)")
	cert := flags.String("cert", os.Getenv("MAIL_PROXY_TLS_CERT"), "certificate chain PEM path")
	key := flags.String("key", os.Getenv("MAIL_PROXY_TLS_KEY"), "private-key PEM path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected certificate setup arguments")
	}
	cfg := config.Config{MailProxyHostname: strings.TrimSpace(*host), IMAPProxyHostname: strings.TrimSpace(*imapHost), SMTPProxyHostname: strings.TrimSpace(*smtpHost)}
	for name, target := range map[string]*bool{"IMAP_PROXY_ENABLED": &cfg.IMAPProxyEnabled, "SMTP_PROXY_ENABLED": &cfg.SMTPProxyEnabled} {
		*target = true
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid %s", name)
			}
			*target = parsed
		}
	}
	var hosts []string
	if cfg.IMAPProxyEnabled || cfg.SMTPProxyEnabled {
		hosts = cfg.MailProxyHostnames()
	}
	if len(hosts) == 0 {
		fmt.Fprintln(output, "Mail proxies disabled: MAIL_PROXY_HOSTNAME is not specified. No TLS certificate was generated.")
		return nil
	}
	info, err := mailproxy.EnsureCertificate(hosts[0], *cert, *key, hosts[1:]...)
	if err != nil {
		return err
	}
	if info.Created {
		fmt.Fprintf(output, "Created a local TLS certificate for %s.\n", strings.Join(hosts, ", "))
	} else {
		fmt.Fprintf(output, "Using the existing TLS certificate for %s.\n", strings.Join(hosts, ", "))
	}
	fmt.Fprintf(output, "SHA-256 fingerprint: %s\nValid until: %s\n", info.Fingerprint, info.Expires.Format(time.RFC3339))
	if info.SelfSigned {
		fmt.Fprintln(output, "This certificate is self-signed: explicitly trust it in your mail clients, or supply a CA-issued certificate.")
	}
	return nil
}
