package mailproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/google/uuid"
)

type Service struct {
	Config      config.Config
	Store       *filestore.Store
	Index       *filestore.UserIndex
	Users       *user.Service
	Policy      outbound.Policy
	Operations  *operations.Registry
	Logger      *slog.Logger
	RootCAs     *x509.CertPool // nil uses the system trust store, including in production.
	mu          sync.Mutex
	listeners   []net.Listener
	connections map[net.Conn]struct{}
	byUser      map[uuid.UUID]map[net.Conn]context.CancelFunc
	failures    map[string]time.Time
	closed      bool
	workers     sync.WaitGroup
	kdfSlots    chan struct{}
	dataSlots   chan struct{}
	smtpServer  *gosmtp.Server
}

func New(cfg config.Config, store *filestore.Store, index *filestore.UserIndex, users *user.Service, policy outbound.Policy, ops *operations.Registry, logger *slog.Logger) *Service {
	return &Service{Config: cfg, Store: store, Index: index, Users: users, Policy: policy, Operations: ops, Logger: logger, connections: make(map[net.Conn]struct{}), byUser: make(map[uuid.UUID]map[net.Conn]context.CancelFunc), failures: make(map[string]time.Time), kdfSlots: make(chan struct{}, 4), dataSlots: make(chan struct{}, 4)}
}

// Start binds all enabled listeners before returning, so invalid certificates
// or occupied ports fail startup rather than leaving half a gateway running.
func (s *Service) Start() error {
	if !s.Config.IMAPProxyEnabled && !s.Config.SMTPProxyEnabled {
		return nil
	}
	hosts := s.Config.MailProxyHostnames()
	if len(hosts) == 0 {
		return errors.New("provide an IMAP or SMTP proxy hostname")
	}
	certificate, err := EnsureCertificate(hosts[0], s.Config.MailProxyCertFile, s.Config.MailProxyKeyFile, hosts[1:]...)
	if err != nil {
		return err
	}
	if certificate.Created && s.Logger != nil {
		s.Logger.Info("generated local mail proxy TLS certificate; trust it in your mail clients", "hostnames", hosts, "sha256", certificate.Fingerprint)
	}
	tlsConfig, err := LoadTLSConfig(s.Config)
	if err != nil {
		return err
	}
	options := []struct {
		enabled           bool
		protocol, address string
	}{{s.Config.IMAPProxyEnabled, "imap", s.Config.IMAPProxyAddress}, {s.Config.SMTPProxyEnabled, "smtp", s.Config.SMTPProxyAddress}}
	for _, option := range options {
		if !option.enabled {
			continue
		}
		listener, listenErr := net.Listen("tcp", option.address)
		if listenErr != nil {
			s.Close()
			return listenErr
		}
		s.mu.Lock()
		s.listeners = append(s.listeners, listener)
		s.workers.Add(1)
		s.mu.Unlock()
		if option.protocol == "smtp" {
			s.smtpServer = s.newSMTPServer()
			go func() {
				defer s.workers.Done()
				_ = s.smtpServer.Serve(tls.NewListener(&smtpListener{Listener: listener, service: s}, tlsConfig))
			}()
		} else {
			go s.serve(listener, option.protocol, tlsConfig)
		}
		if s.Logger != nil {
			s.Logger.Info("mail proxy listening", "protocol", option.protocol, "address", option.address)
		}
	}
	return nil
}

func (s *Service) serve(listener net.Listener, protocol string, tlsConfig *tls.Config) {
	defer s.workers.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed || len(s.connections) >= 128 {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.connections[conn] = struct{}{}
		s.workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.workers.Done()
			defer conn.Close()
			defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			connection := s.gatewayOperation(protocol, "connection", conn)
			connection.Outcome = "accepted"
			connection.End(nil)
			defer func() { connection.Outcome = "closed"; connection.End(nil) }()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			secure := tls.Server(conn, tlsConfig)
			handshake := s.gatewayOperation(protocol, "tls", conn)
			err := secure.Handshake()
			handshake.End(err)
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
			s.handleIMAP(conn, secure)
		}()
	}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	listeners := append([]net.Listener(nil), s.listeners...)
	var connections []net.Conn
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	for id := range s.byUser {
		s.closeUserLocked(id)
	}
	s.mu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	if s.smtpServer != nil {
		_ = s.smtpServer.Close()
	}
	s.workers.Wait()
}

func (s *Service) closeUserLocked(id uuid.UUID) {
	for _, cancel := range s.byUser[id] {
		cancel()
	}
	delete(s.byUser, id)
}

func (s *Service) authenticate(conn net.Conn, login, password, protocol string) (uuid.UUID, Target, string, error) {
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	s.mu.Lock()
	last := s.failures[ip]
	blocked := time.Since(last) < 30*time.Second
	if len(s.failures) >= 10000 {
		for key, when := range s.failures {
			if time.Since(when) >= 30*time.Second {
				delete(s.failures, key)
			}
		}
		if len(s.failures) >= 10000 {
			blocked = true
		}
	}
	s.mu.Unlock()
	if blocked {
		return uuid.Nil, Target{}, "", ErrAuthentication
	}
	select {
	case s.kdfSlots <- struct{}{}:
		defer func() { <-s.kdfSlots }()
	default:
		return uuid.Nil, Target{}, "", errors.New("authentication busy")
	}
	id, target, revision, err := s.unlock(login, password, protocol)
	if err != nil {
		s.mu.Lock()
		s.failures[ip] = time.Now()
		s.mu.Unlock()
	}
	return id, target, revision, err
}

type upstream struct {
	reader io.Reader
	writer io.Writer
	close  func() error
	conn   net.Conn
}

func (s *Service) relay(raw, secure net.Conn, clientReader *bufio.Reader, id uuid.UUID, revision, protocol string, target Target, accept func() error) (resultErr error) {
	defer s.gatewayOperation(protocol, "relay", raw).Result(&resultErr)
	lifetime := s.Config.SessionMaxLifetime
	if lifetime <= 0 {
		lifetime = 12 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), lifetime)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if s.Operations != nil {
		defer s.Operations.Track(id, cancel)()
	}
	s.mu.Lock()
	value, current, err := s.read(id)
	if s.closed || err != nil || current != revision || !s.Users.IsActive(id) || !s.current(id, protocol, value) || len(s.byUser[id]) >= 16 {
		s.mu.Unlock()
		return ErrAuthentication
	}
	if s.byUser[id] == nil {
		s.byUser[id] = make(map[net.Conn]context.CancelFunc)
	}
	s.byUser[id][raw] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.byUser[id], raw); s.mu.Unlock() }()
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	remote, err := s.connectIMAP(dialCtx, target)
	dialCancel()
	if err != nil {
		return errors.New("upstream mail connection failed")
	}
	defer remote.close()
	stopRemote := context.AfterFunc(ctx, func() { _ = remote.close() })
	defer stopRemote()
	if ctx.Err() != nil || !s.Users.IsActive(id) {
		return ErrAuthentication
	}
	if err = accept(); err != nil {
		return err
	}
	active := s.gatewayOperation(protocol, "relay", raw)
	active.Outcome = "started"
	active.End(nil)
	_ = secure.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(deadlineWriter{remote.writer, remote.conn}, deadlineReader{clientReader, secure})
		_ = remote.close()
	}()
	_, _ = io.Copy(deadlineWriter{secure, secure}, deadlineReader{remote.reader, remote.conn})
	_ = raw.Close()
	_ = remote.close()
	<-done
	return nil
}

func (s *Service) tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: s.RootCAs}
}

// Deadlines are renewed on traffic. IMAP clients renew IDLE within 29 minutes.
type deadlineReader struct {
	io.Reader
	conn net.Conn
}

func (r deadlineReader) Read(p []byte) (int, error) {
	_ = r.conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	return r.Reader.Read(p)
}

type deadlineWriter struct {
	io.Writer
	conn net.Conn
}

func (w deadlineWriter) Write(p []byte) (int, error) {
	_ = w.conn.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	return w.Writer.Write(p)
}
