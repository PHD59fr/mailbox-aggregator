package imap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"mime"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/example/mail-aggregator/internal/mailsasl"
	"github.com/example/mail-aggregator/internal/outbound"
)

type Configuration struct {
	Host               string
	Mailbox            string
	TrashMailbox       string
	TrashExcluded      []string
	SentMailbox        string
	SentExcluded       []string
	DraftsMailbox      string
	DraftsExcluded     []string
	Search, Seen, From string
	Expression         string
	UIDs               []uint32
	Flagged            bool
	To, Subject        string
	Since, Before      time.Time
	Port               uint16
	Security, Username string
	Password           []byte
	OAuthToken         []byte
	SASLMechanism      string
}

func (cfg Configuration) mailbox() string {
	if cfg.Mailbox == "" {
		return "INBOX"
	}
	return cfg.Mailbox
}

type Status struct {
	Messages    uint32 `json:"messages"`
	Unseen      uint32 `json:"unseen"`
	UIDNext     uint32 `json:"uid_next"`
	UIDValidity uint32 `json:"uid_validity"`
	Mode        string `json:"mode"`
	Error       string `json:"error,omitempty"`
}
type Receiver struct {
	responseBytes int64
	Policy        outbound.Policy
	RootCAs       *x509.CertPool
	Logger        *slog.Logger
}

var ErrAuthentication = errors.New("IMAP authentication failed")

func (r Receiver) Test(ctx context.Context, cfg Configuration) (resultErr error) {
	defer r.operation(ctx, cfg, "test").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return err
	}
	closeConn()
	return nil
}

// Watch prefers IDLE and falls back to bounded, short-lived STATUS connections.
// It fetches counts only, never message bodies, and never changes message flags.
func (r Receiver) Watch(ctx context.Context, cfg Configuration, interval time.Duration, emit func(Status)) {
	watch := r.operation(ctx, cfg, "watch")
	watch.Outcome = "started"
	watch.End(nil)
	defer func() {
		watch.Outcome = "stopped"
		watch.End(nil)
	}()
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 5*time.Minute {
		interval = 5 * time.Minute
	}
	err := r.idle(ctx, cfg, emit)
	if ctx.Err() != nil {
		return
	}
	if errors.Is(err, ErrAuthentication) {
		emit(Status{Mode: "polling", Error: "IMAP sign-in failed. Update the password in Settings, then refresh."})
		<-ctx.Done()
		return
	}
	for ctx.Err() == nil {
		status, err := r.poll(ctx, cfg)
		status.Mode = "polling"
		if err != nil {
			status.Error = "Could not check this mailbox. Verify its IMAP settings and password."
		}
		emit(status)
		if errors.Is(err, ErrAuthentication) {
			<-ctx.Done()
			return
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r Receiver) connect(ctx context.Context, cfg Configuration, changed chan<- struct{}) (result *imapclient.Client, cleanup func(), resultErr error) {
	op := r.operation(ctx, cfg, "connect")
	op.Stage = "dial"
	defer op.Result(&resultErr)
	setupCtx, cancelSetup := context.WithTimeout(ctx, 20*time.Second)
	defer cancelSetup()
	raw, err := r.Policy.DialContext(setupCtx, "tcp", cfg.Host, cfg.Port)
	if err != nil {
		return nil, nil, err
	}
	if r.responseBytes > 0 {
		raw = &responseLimitConn{Conn: raw, remaining: r.responseBytes}
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	stopSetup := context.AfterFunc(setupCtx, func() { _ = raw.Close() })
	closeConn := func() { stopSetup(); stop(); _ = raw.Close() }
	options := &imapclient.Options{
		TLSConfig:   &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: r.RootCAs},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}
	if changed != nil {
		notify := func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
		options.UnilateralDataHandler = &imapclient.UnilateralDataHandler{Mailbox: func(*imapclient.UnilateralDataMailbox) { notify() }, Fetch: func(*imapclient.FetchMessageData) { notify() }, Expunge: func(uint32) { notify() }}
	}
	var client *imapclient.Client
	op.Stage = "tls"
	switch cfg.Security {
	case "tls":
		secured := tls.Client(raw, options.TLSConfig)
		if err = secured.HandshakeContext(setupCtx); err == nil {
			client = imapclient.New(secured, options)
		}
	case "starttls":
		client, err = imapclient.NewStartTLS(raw, options)
	default:
		err = errors.New("IMAP requires verified TLS or STARTTLS")
	}
	if err != nil {
		closeConn()
		return nil, nil, err
	}
	op.Stage = "authenticate"
	if len(cfg.OAuthToken) > 0 {
		err = client.Authenticate(&mailsasl.XOAuth2{Username: cfg.Username, Token: cfg.OAuthToken})
	} else if cfg.SASLMechanism == "SCRAM-SHA-256" {
		scram := &mailsasl.SCRAM256{Username: cfg.Username, Password: cfg.Password}
		if !client.Caps().Has(protocol.Cap("AUTH=SCRAM-SHA-256")) {
			err = ErrAuthentication
		} else {
			err = client.Authenticate(scram)
			if err == nil && !scram.Valid() {
				err = ErrAuthentication
			}
		}
	} else if cfg.SASLMechanism != "" {
		err = ErrAuthentication
	} else {
		err = client.Login(cfg.Username, string(cfg.Password)).Wait()
	}
	if err != nil {
		closeConn()
		_ = client.Close()
		var authenticationError *protocol.Error
		if errors.As(err, &authenticationError) {
			return nil, nil, ErrAuthentication
		}
		return nil, nil, err
	}
	stopSetup()
	op.Stage = "ready"
	return client, func() { closeConn(); _ = client.Close() }, nil
}

func readStatus(client *imapclient.Client) (Status, error) {
	data, err := client.Status("INBOX", &protocol.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true, UIDValidity: true}).Wait()
	if err != nil {
		return Status{}, err
	}
	if data.NumMessages == nil || data.NumUnseen == nil {
		return Status{}, errors.New("incomplete IMAP status")
	}
	return Status{Messages: *data.NumMessages, Unseen: *data.NumUnseen, UIDNext: uint32(data.UIDNext), UIDValidity: data.UIDValidity}, nil
}

func (r Receiver) poll(ctx context.Context, cfg Configuration) (result Status, resultErr error) {
	defer r.operation(ctx, cfg, "poll").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return Status{}, err
	}
	defer closeConn()
	return readStatus(client)
}

func (r Receiver) idle(ctx context.Context, cfg Configuration, emit func(Status)) (resultErr error) {
	defer r.operation(ctx, cfg, "idle").Result(&resultErr)
	changed := make(chan struct{}, 1)
	client, closeConn, err := r.connect(ctx, cfg, changed)
	if err != nil {
		return err
	}
	defer closeConn()
	if !client.Caps().Has(protocol.CapIdle) {
		return errors.New("IDLE unavailable")
	}
	if _, err = client.Select("INBOX", &protocol.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return err
	}
	for ctx.Err() == nil {
		// Drain before STATUS: a change arriving during or after its response
		// must remain queued so the next IDLE cycle cannot miss new mail.
		select {
		case <-changed:
		default:
		}
		status, err := readStatus(client)
		if err != nil {
			return err
		}
		status.Mode = "idle"
		emit(status)
		idle, err := client.Idle()
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- idle.Wait() }()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-done:
			_ = idle.Close()
			return err
		case <-changed:
			if err = idle.Close(); err != nil {
				return err
			}
			if err = <-done; err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
