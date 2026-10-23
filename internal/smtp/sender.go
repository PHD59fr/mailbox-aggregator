package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-sasl"
	smtpclient "github.com/emersion/go-smtp"
	"github.com/example/mail-aggregator/internal/maillog"
	"github.com/example/mail-aggregator/internal/mailsasl"
	"github.com/example/mail-aggregator/internal/outbound"
)

type Configuration struct {
	Host       string
	Port       uint16
	Security   string
	Username   string
	Password   []byte
	OAuthToken []byte
}
type Sender struct {
	Policy           outbound.Policy
	OperationTimeout time.Duration
	RootCAs          *x509.CertPool
	Logger           *slog.Logger
}

var ErrSubmissionUnknown = errors.New("SMTP submission outcome is ambiguous")

func (s Sender) Send(ctx context.Context, cfg Configuration, message BuiltMessage) (resultErr error) {
	op := s.operation(ctx, cfg, "send")
	op.Stage = "validate"
	defer func() {
		if errors.Is(resultErr, ErrSubmissionUnknown) {
			op.Outcome = "unknown"
		}
		op.End(resultErr)
	}()
	if len(message.Data) == 0 || message.EnvelopeFrom == "" || len(message.Recipients) == 0 {
		return errors.New("invalid message")
	}
	op.Stage = "connect"
	client, closeConn, err := s.connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeConn()
	return submit(client, message, op)
}

func (s Sender) Test(ctx context.Context, cfg Configuration) (resultErr error) {
	defer s.operation(ctx, cfg, "test").Result(&resultErr)
	client, closeConn, err := s.connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeConn()
	_ = client.Quit()
	return nil
}

func (s Sender) connect(ctx context.Context, cfg Configuration) (client *smtpclient.Client, closeConn func(), err error) {
	op := s.operation(ctx, cfg, "connect")
	op.Stage = "dial"
	defer op.Result(&err)
	closeConn = func() {}
	success := false
	timeout := s.OperationTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	dialContext, cancelDial := context.WithDeadline(ctx, deadline)
	defer cancelDial()
	raw, err := s.Policy.DialContext(dialContext, "tcp", cfg.Host, cfg.Port)
	if err != nil {
		return nil, closeConn, fmt.Errorf("connect to SMTP server: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	closeConn = func() { stop(); _ = raw.Close() }
	defer func() {
		if !success {
			closeConn()
		}
	}()
	bounded := &deadlineConn{Conn: raw, limit: deadline}
	if err = bounded.SetDeadline(deadline); err != nil {
		return nil, closeConn, errors.New("set SMTP deadline")
	}
	tlsConfig := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: s.RootCAs}
	op.Stage = "tls"
	switch cfg.Security {
	case "tls":
		secured := tls.Client(bounded, tlsConfig)
		if err = secured.HandshakeContext(ctx); err != nil {
			return nil, closeConn, errors.New("SMTP TLS handshake failed")
		}
		client = smtpclient.NewClient(secured)
	case "starttls":
		client, err = smtpclient.NewClientStartTLS(bounded, tlsConfig)
	default:
		return nil, closeConn, errors.New("SMTP requires TLS or STARTTLS")
	}
	if err != nil {
		return nil, closeConn, errors.New("SMTP negotiation failed")
	}
	client.CommandTimeout = timeout
	client.SubmissionTimeout = timeout
	op.Stage = "greeting"
	if err = client.Hello("localhost"); err != nil {
		return nil, closeConn, errors.New("SMTP greeting failed")
	}
	state, secured := client.TLSConnectionState()
	if !secured || !state.HandshakeComplete {
		return nil, closeConn, errors.New("SMTP requires verified TLS before authentication")
	}
	var auth sasl.Client
	op.Stage = "authenticate"
	switch {
	case len(cfg.OAuthToken) > 0:
		if !client.SupportsAuth("XOAUTH2") {
			return nil, closeConn, errors.New("SMTP OAuth authentication is unsupported")
		}
		auth = &mailsasl.XOAuth2{Username: cfg.Username, Token: cfg.OAuthToken}
	case client.SupportsAuth(sasl.Plain):
		auth = sasl.NewPlainClient("", cfg.Username, string(cfg.Password))
	case client.SupportsAuth(sasl.Login):
		auth = sasl.NewLoginClient(cfg.Username, string(cfg.Password))
	default:
		return nil, closeConn, errors.New("SMTP authentication mechanism unsupported")
	}
	if err = client.Auth(auth); err != nil {
		return nil, closeConn, errors.New("SMTP authentication failed")
	}
	success = true
	op.Stage = "ready"
	return client, closeConn, nil
}

func (s Sender) operation(ctx context.Context, cfg Configuration, name string) *maillog.Operation {
	return maillog.Start(ctx, s.Logger, "smtp", "provider", name, "host", cfg.Host, "port", cfg.Port, "security", cfg.Security)
}

func submit(client *smtpclient.Client, message BuiltMessage, op *maillog.Operation) error {
	op.Stage = "sender"
	if err := client.Mail(message.EnvelopeFrom, nil); err != nil {
		return errors.New("SMTP sender rejected")
	}
	op.Stage = "recipient"
	for _, recipient := range message.Recipients {
		if err := client.Rcpt(recipient, nil); err != nil {
			return errors.New("SMTP recipient rejected")
		}
	}
	op.Stage = "data"
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP DATA rejected")
	}
	if _, err = writer.Write(message.Data); err != nil {
		return ErrSubmissionUnknown
	}
	if err = writer.Close(); err != nil {
		return ErrSubmissionUnknown
	}
	// DATA has been accepted. A failed QUIT must not turn a sent message into
	// a reported failure that could cause the user to submit it twice.
	_ = client.Quit()
	op.Stage = "accepted"
	return nil
}

func ConnectionAddress(host string, port uint16) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

// The maintained client sets/reset deadlines per command. Clamp those updates
// to one absolute operation budget so DATA, AUTH and QUIT cannot extend it.
type deadlineConn struct {
	net.Conn
	limit time.Time
}

func (c *deadlineConn) bounded(value time.Time) time.Time {
	if value.IsZero() || value.After(c.limit) {
		return c.limit
	}
	return value
}
func (c *deadlineConn) SetDeadline(value time.Time) error {
	return c.Conn.SetDeadline(c.bounded(value))
}
func (c *deadlineConn) SetReadDeadline(value time.Time) error {
	return c.Conn.SetReadDeadline(c.bounded(value))
}
func (c *deadlineConn) SetWriteDeadline(value time.Time) error {
	return c.Conn.SetWriteDeadline(c.bounded(value))
}
