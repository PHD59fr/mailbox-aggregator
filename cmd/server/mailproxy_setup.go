package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/mailproxy"
)

func ensureMailProxyCertificate(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("ensure-mail-proxy-certificate", flag.ContinueOnError)
	flags.SetOutput(output)
	host := flags.String("hostname", os.Getenv("MAIL_PROXY_HOSTNAME"), "DNS hostname or IP used by mail clients")
	cert := flags.String("cert", os.Getenv("MAIL_PROXY_TLS_CERT"), "certificate chain PEM path")
	key := flags.String("key", os.Getenv("MAIL_PROXY_TLS_KEY"), "private-key PEM path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected certificate setup arguments")
	}
	if strings.TrimSpace(*host) == "" {
		fmt.Fprintln(output, "Mail proxies disabled: MAIL_PROXY_HOSTNAME is not specified. No TLS certificate was generated.")
		return nil
	}
	info, err := mailproxy.EnsureCertificate(*host, *cert, *key)
	if err != nil {
		return err
	}
	if info.Created {
		fmt.Fprintf(output, "Created a local TLS certificate for %s.\n", *host)
	} else {
		fmt.Fprintf(output, "Using the existing TLS certificate for %s.\n", *host)
	}
	fmt.Fprintf(output, "SHA-256 fingerprint: %s\nValid until: %s\n", info.Fingerprint, info.Expires.Format(time.RFC3339))
	if info.SelfSigned {
		fmt.Fprintln(output, "This certificate is self-signed: explicitly trust it in your mail clients, or supply a CA-issued certificate.")
	}
	return nil
}
