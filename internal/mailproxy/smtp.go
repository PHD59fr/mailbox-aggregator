package mailproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
)

const maxProxyMessage = 20 << 20

// SMTP terminates locally so MAIL FROM and message authors can be checked
// against this user's encrypted sender snapshot before any upstream submission.
func (s *Service) newSMTPServer() *gosmtp.Server {
	server := gosmtp.NewServer(gosmtp.BackendFunc(func(conn *gosmtp.Conn) (gosmtp.Session, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return nil, errors.New("proxy stopped")
		}
		s.workers.Add(1)
		return &smtpSession{service: s, conn: conn}, nil
	}))
	server.Domain = s.Config.MailProxyHostname
	server.MaxMessageBytes = maxProxyMessage
	server.MaxRecipients = 100
	server.MaxLineLength = maxLine
	server.ReadTimeout = 5 * time.Minute
	server.WriteTimeout = 30 * time.Second
	// The transport is an implicit TLS listener. Do not offer a cleartext mode.
	server.AllowInsecureAuth = false
	// No wire logging: protocol lines and backend details may contain secrets.
	server.ErrorLog = log.New(io.Discard, "", 0)
	return server
}

type smtpListener struct {
	net.Listener
	service *Service
}

func (l *smtpListener) Accept() (net.Conn, error) {
	for {
		raw, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		s := l.service
		s.mu.Lock()
		if s.closed || len(s.connections) >= 128 {
			s.mu.Unlock()
			_ = raw.Close()
			continue
		}
		conn := &smtpTrackedConn{Conn: raw, service: s}
		s.connections[conn] = struct{}{}
		s.workers.Add(1)
		s.mu.Unlock()
		return conn, nil
	}
}

type smtpTrackedConn struct {
	net.Conn
	service *Service
	once    sync.Once
}

func (c *smtpTrackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.service.mu.Lock()
		delete(c.service.connections, c)
		c.service.mu.Unlock()
		c.service.workers.Done()
	})
	return err
}

type smtpSession struct {
	service    *Service
	conn       *gosmtp.Conn
	mu         sync.Mutex
	once       sync.Once
	userID     uuid.UUID
	revision   string
	routes     map[string]Target
	ctx        context.Context
	cancel     context.CancelFunc
	stop       func() bool
	untrack    func()
	from       string
	recipients []string
	selected   Target
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain, sasl.Login} }
func (s *smtpSession) Auth(mechanism string) (sasl.Server, error) {
	authenticate := func(username, password string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		id, target, revision, err := s.service.authenticate(s.conn.Conn(), username, password, "smtp")
		if err != nil {
			return gosmtp.ErrAuthFailed
		}
		lifetime := s.service.Config.SessionMaxLifetime
		if lifetime <= 0 {
			lifetime = 12 * time.Hour
		}
		ctx, cancel := context.WithTimeout(context.Background(), lifetime)
		stop := context.AfterFunc(ctx, func() { _ = s.conn.Conn().Close() })
		untrack := func() {}
		if s.service.Operations != nil {
			untrack = s.service.Operations.Track(id, cancel)
		}
		s.service.mu.Lock()
		value, current, readErr := s.service.read(id)
		if s.service.closed || readErr != nil || current != revision || !s.service.Users.IsActive(id) || !s.service.current(id, "smtp", value) || len(s.service.byUser[id]) >= 16 {
			s.service.mu.Unlock()
			stop()
			cancel()
			untrack()
			return gosmtp.ErrAuthFailed
		}
		if s.service.byUser[id] == nil {
			s.service.byUser[id] = make(map[net.Conn]context.CancelFunc)
		}
		s.service.byUser[id][s.conn.Conn()] = cancel
		s.service.mu.Unlock()
		s.userID, s.revision, s.ctx, s.cancel, s.stop, s.untrack = id, revision, ctx, cancel, stop, untrack
		s.routes = make(map[string]Target, len(target.routes))
		for _, sender := range target.routes {
			s.routes[strings.ToLower(sender.SenderEmail)] = sender
		}
		return nil
	}
	if mechanism == sasl.Plain {
		return sasl.NewPlainServer(func(identity, username, password string) error {
			if identity != "" && identity != username {
				return gosmtp.ErrAuthFailed
			}
			return authenticate(username, password)
		}), nil
	}
	if mechanism == sasl.Login {
		return &loginServer{authenticate: authenticate}, nil
	}
	return nil, gosmtp.ErrAuthUnsupported
}

type loginServer struct {
	step         int
	username     string
	authenticate func(string, string) error
}

func (s *loginServer) Next(response []byte) ([]byte, bool, error) {
	if len(response) > 1024 {
		return nil, true, gosmtp.ErrAuthFailed
	}
	if s.step == 0 && len(response) == 0 {
		s.step = 1
		return []byte("Username:"), false, nil
	}
	if s.step <= 1 {
		s.username = string(response)
		s.step = 2
		return []byte("Password:"), false, nil
	}
	if s.step == 2 {
		s.step = 3
		err := s.authenticate(s.username, string(response))
		s.username = ""
		return nil, true, err
	}
	return nil, true, gosmtp.ErrAuthFailed
}

func (s *smtpSession) authorized() bool {
	if s.ctx == nil || s.ctx.Err() != nil || !s.service.Users.IsActive(s.userID) {
		return false
	}
	value, revision, err := s.service.read(s.userID)
	return err == nil && revision == s.revision && s.service.current(s.userID, "smtp", value)
}
func smtpError(code int, message string) error {
	return &gosmtp.SMTPError{Code: code, Message: message}
}

func (s *smtpSession) Mail(from string, options *gosmtp.MailOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorized() {
		return gosmtp.ErrAuthRequired
	}
	s.resetLocked()
	target, ok := s.routes[strings.ToLower(from)]
	if !ok {
		return smtpError(550, "Sender address is not enabled for this account")
	}
	if options.UTF8 || options.Body == gosmtp.BodyBinaryMIME {
		return smtpError(554, "Unsupported message transport")
	}
	s.from, s.selected = target.SenderEmail, target
	return nil
}
func (s *smtpSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorized() {
		return gosmtp.ErrAuthRequired
	}
	if s.from == "" {
		return smtpError(503, "Choose a sender first")
	}
	if len(s.recipients) >= 100 {
		return smtpError(452, "Too many recipients")
	}
	s.recipients = append(s.recipients, to)
	return nil
}

func validAuthor(data []byte, from string) bool {
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		headerEnd = bytes.Index(data, []byte("\n\n"))
	}
	if headerEnd < 0 || headerEnd > 64<<10 {
		return false
	}
	message, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return false
	}
	for _, name := range []string{"From", "Sender", "Resent-From", "Resent-Sender"} {
		values := message.Header[name]
		if len(values) == 0 && name != "From" {
			continue
		}
		if len(values) != 1 {
			return false
		}
		addresses, err := mail.ParseAddressList(values[0])
		if err != nil || len(addresses) != 1 || !strings.EqualFold(addresses[0].Address, from) {
			return false
		}
	}
	return true
}

func (s *smtpSession) Data(reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.resetLocked()
	if !s.authorized() {
		return gosmtp.ErrAuthRequired
	}
	if s.from == "" || len(s.recipients) == 0 {
		return smtpError(503, "Sender and recipients required")
	}
	select {
	case s.service.dataSlots <- struct{}{}:
		defer func() { <-s.service.dataSlots }()
	default:
		return smtpError(451, "Submission busy; try again later")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxProxyMessage+1))
	if err != nil {
		return smtpError(451, "Could not read message")
	}
	defer clear(data)
	if len(data) > maxProxyMessage {
		return smtpError(552, "Message too large")
	}
	if !validAuthor(data, s.from) {
		return smtpError(550, "Message From and Sender must match the authorized sender address")
	}
	if !s.authorized() {
		return gosmtp.ErrAuthRequired
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	client, raw, err := s.service.connectSMTP(ctx, s.selected)
	if err != nil {
		return smtpError(451, "Upstream sender unavailable")
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if err = client.Mail(s.from, &gosmtp.MailOptions{Body: gosmtp.Body8BitMIME, Size: int64(len(data))}); err != nil {
		return smtpError(554, "Provider rejected sender")
	}
	for _, recipient := range s.recipients {
		if err = client.Rcpt(recipient, nil); err != nil {
			return smtpError(554, "Provider rejected recipient")
		}
	}
	writer, err := client.Data()
	if err != nil {
		return smtpError(451, "Provider did not accept message data")
	}
	if _, err = writer.Write(data); err != nil {
		return smtpError(451, "Submission interrupted; delivery may be uncertain")
	}
	if err = writer.Close(); err != nil {
		return smtpError(451, "Submission was not confirmed; delivery may be uncertain")
	}
	return nil
}

func (s *smtpSession) resetLocked() { s.from = ""; s.selected = Target{}; s.recipients = nil }
func (s *smtpSession) Reset()       { s.mu.Lock(); defer s.mu.Unlock(); s.resetLocked() }
func (s *smtpSession) Logout() error {
	s.once.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stop != nil {
			s.stop()
		}
		if s.cancel != nil {
			s.cancel()
		}
		if s.untrack != nil {
			s.untrack()
		}
		s.service.mu.Lock()
		delete(s.service.byUser[s.userID], s.conn.Conn())
		s.service.mu.Unlock()
		s.routes = nil
		s.resetLocked()
		s.service.workers.Done()
	})
	return nil
}

func (s *Service) connectSMTP(ctx context.Context, target Target) (*gosmtp.Client, net.Conn, error) {
	raw, err := s.Policy.DialContext(ctx, "tcp", target.Host, target.Port)
	if err != nil {
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	var conn net.Conn = raw
	if target.Security == "tls" {
		secure := tls.Client(raw, s.tlsConfig(target.Host))
		if err = secure.HandshakeContext(ctx); err != nil {
			return nil, nil, err
		}
		conn = secure
	}
	var client *gosmtp.Client
	if target.Security == "starttls" {
		client, err = gosmtp.NewClientStartTLS(conn, s.tlsConfig(target.Host))
	} else {
		client = gosmtp.NewClient(conn)
	}
	if err != nil {
		return nil, nil, err
	}
	client.CommandTimeout = 30 * time.Second
	client.SubmissionTimeout = 2 * time.Minute
	if err = client.Hello("mailbox-aggregator"); err != nil {
		return nil, nil, err
	}
	if err = client.Auth(sasl.NewPlainClient("", target.Username, target.Password)); err != nil {
		return nil, nil, err
	}
	_ = raw.SetDeadline(time.Time{})
	success = true
	return client, raw, nil
}
