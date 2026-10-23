// Package mailproxy provides opt-in TLS mail gateways using revocable,
// password-encrypted copies of the user's selected provider credentials.
package mailproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"strings"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

var ErrAuthentication = errors.New("mail proxy authentication failed")

type Target struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Security    string `json:"security"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	SenderEmail string `json:"sender_email,omitempty"`
	routes      []Target
}

type Targets struct {
	IMAP             *Target  `json:"imap,omitempty"`
	SMTP             *Target  `json:"smtp,omitempty"`
	Senders          []Target `json:"senders,omitempty"`
	AccountsRevision string   `json:"accounts_revision,omitempty"`
	SMTPRevision     string   `json:"smtp_revision,omitempty"`
}

type Status struct {
	IMAPEnabled bool   `json:"imap_available"`
	SMTPEnabled bool   `json:"smtp_available"`
	Hostname    string `json:"hostname"`
	IMAPPort    int    `json:"imap_port"`
	SMTPPort    int    `json:"smtp_port"`
	IMAPID      string `json:"imap_account_id"`
	SMTPID      string `json:"smtp_config_id"`
	Stale       bool   `json:"needs_update"`
}

// No application master key can decrypt this opt-in credential copy. Its key
// exists only while configuring or authenticating with the dedicated password.
type record struct {
	Version          int    `json:"version"`
	IMAPID           string `json:"imap_account_id,omitempty"`
	SMTPID           string `json:"smtp_config_id,omitempty"`
	AccountsRevision string `json:"accounts_revision,omitempty"`
	SMTPRevision     string `json:"smtp_revision,omitempty"`
	Salt             []byte `json:"salt,omitempty"`
	Nonce            []byte `json:"nonce,omitempty"`
	Ciphertext       []byte `json:"ciphertext,omitempty"`
}

func (s *Service) read(id uuid.UUID) (record, string, error) {
	data, revision, err := s.Store.ReadUserFile(id, "proxy.json")
	if errors.Is(err, os.ErrNotExist) {
		return record{Version: 1}, "*", nil
	}
	if err != nil {
		return record{}, "", err
	}
	var value record
	if json.Unmarshal(data, &value) != nil || value.Version != 1 {
		return record{}, "", errors.New("invalid proxy record")
	}
	return value, revision, nil
}

func (s *Service) Status(id uuid.UUID) (Status, string, error) {
	value, revision, err := s.read(id)
	status := Status{IMAPEnabled: s.Config.IMAPProxyEnabled, SMTPEnabled: s.Config.SMTPProxyEnabled, Hostname: s.Config.MailProxyHostname, IMAPPort: 993, SMTPPort: 465, IMAPID: value.IMAPID, SMTPID: value.SMTPID}
	if err == nil {
		status.Stale = (value.IMAPID != "" && !s.current(id, "imap", value)) || (value.SMTPID != "" && !s.current(id, "smtp", value))
	}
	return status, revision, err
}

func (s *Service) current(id uuid.UUID, protocol string, value record) bool {
	name, expected := "accounts.enc", value.AccountsRevision
	if protocol == "smtp" {
		name, expected = "smtp.enc", value.SMTPRevision
	}
	_, actual, err := s.Store.ReadUserFile(id, name)
	return err == nil && actual == expected && expected != ""
}

func validTarget(target *Target) bool {
	return target == nil || (len(target.ID) > 0 && len(target.ID) <= 128 && target.Host != "" && target.Port > 0 && (target.Security == "tls" || target.Security == "starttls") && target.Username != "" && len(target.Username) <= 512 && len(target.Password) > 0 && len(target.Password) <= 4096 && !strings.ContainsAny(target.Username+target.Password, "\x00\r\n"))
}

func validSenders(targets Targets) bool {
	if targets.SMTP != nil && len(targets.Senders) > 0 {
		return false
	}
	senders := targets.Senders
	if targets.SMTP != nil {
		senders = []Target{*targets.SMTP}
	}
	if len(senders) > 20 {
		return false
	}
	seen := make(map[string]bool)
	for i := range senders {
		sender := &senders[i]
		address, err := mail.ParseAddress(sender.SenderEmail)
		key := strings.ToLower(sender.SenderEmail)
		if !validTarget(sender) || err != nil || address.Address != sender.SenderEmail || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func (s *Service) Save(id uuid.UUID, password string, targets Targets, expected string, expectedAuth ...string) (string, error) {
	if !validTarget(targets.IMAP) || !validSenders(targets) || (targets.IMAP != nil && !s.Config.IMAPProxyEnabled) || ((targets.SMTP != nil || len(targets.Senders) > 0) && !s.Config.SMTPProxyEnabled) {
		return "", errors.New("invalid or unavailable proxy target")
	}
	value := record{Version: 1}
	if targets.IMAP != nil || targets.SMTP != nil || len(targets.Senders) > 0 {
		select {
		case s.kdfSlots <- struct{}{}:
			defer func() { <-s.kdfSlots }()
		default:
			return "", errors.New("proxy settings busy")
		}
		if len(password) < 16 || len(password) > 1024 || strings.ContainsAny(password, "\x00\r\n") {
			return "", errors.New("use a dedicated proxy password of 16 to 1024 bytes")
		}
		var err error
		if targets.IMAP != nil {
			value.IMAPID = targets.IMAP.ID
			_, value.AccountsRevision, err = s.Store.ReadUserFile(id, "accounts.enc")
			if err != nil {
				return "", err
			}
		}
		if targets.SMTP != nil || len(targets.Senders) > 0 {
			value.SMTPID = "*"
			if targets.SMTP != nil {
				value.SMTPID = targets.SMTP.ID
			}
			_, value.SMTPRevision, err = s.Store.ReadUserFile(id, "smtp.enc")
			if err != nil {
				return "", err
			}
		}
		if (targets.AccountsRevision != "" && targets.AccountsRevision != value.AccountsRevision) || (targets.SMTPRevision != "" && targets.SMTPRevision != value.SMTPRevision) {
			return "", filestore.ErrConflict
		}
		value.Salt = make([]byte, 16)
		value.Nonce = make([]byte, 12)
		if _, err = rand.Read(value.Salt); err != nil {
			return "", err
		}
		if _, err = rand.Read(value.Nonce); err != nil {
			return "", err
		}
		plain, err := json.Marshal(targets)
		if err != nil {
			return "", err
		}
		defer clear(plain)
		key := proxyKey(password, value.Salt)
		defer clear(key)
		block, err := aes.NewCipher(key)
		if err != nil {
			return "", err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return "", err
		}
		value.Ciphertext = aead.Seal(nil, value.Nonce, plain, associatedData(id, value))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Users.IsActive(id) {
		return "", ErrAuthentication
	}
	if (targets.IMAP != nil && !s.current(id, "imap", value)) || ((targets.SMTP != nil || len(targets.Senders) > 0) && !s.current(id, "smtp", value)) {
		return "", filestore.ErrConflict
	}
	revision, err := s.Store.WriteUserFile(id, "proxy.json", data, expected, expectedAuth...)
	if err == nil {
		s.closeUserLocked(id)
	}
	return revision, err
}

func proxyKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
}
func associatedData(id uuid.UUID, value record) []byte {
	data, _ := json.Marshal([]string{"mailbox-proxy-v1", id.String(), value.IMAPID, value.SMTPID, value.AccountsRevision, value.SMTPRevision})
	return data
}

func (s *Service) unlock(login, password, protocol string) (uuid.UUID, Target, string, error) {
	entry, ok := s.Index.Lookup(login)
	id := entry.UserID
	value, revision, err := s.read(id)
	if !ok || err != nil || !s.Users.IsActive(id) || len(value.Salt) != 16 || len(value.Nonce) != 12 || len(value.Ciphertext) < 16 || len(password) > 1024 {
		// Perform the same expensive KDF for missing/disabled users.
		key := proxyKey(password, make([]byte, 16))
		clear(key)
		return uuid.Nil, Target{}, "", ErrAuthentication
	}
	key := proxyKey(password, value.Salt)
	defer clear(key)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	plain, err := aead.Open(nil, value.Nonce, value.Ciphertext, associatedData(id, value))
	if err != nil {
		return uuid.Nil, Target{}, "", ErrAuthentication
	}
	defer clear(plain)
	var targets Targets
	if json.Unmarshal(plain, &targets) != nil {
		return uuid.Nil, Target{}, "", ErrAuthentication
	}
	target := targets.IMAP
	expected := value.IMAPID
	if protocol == "smtp" {
		target = targets.SMTP
		expected = value.SMTPID
		if !validSenders(targets) {
			return uuid.Nil, Target{}, "", ErrAuthentication
		}
		if len(targets.Senders) > 0 {
			target = &targets.Senders[0]
			expected = target.ID
			target.routes = append([]Target(nil), targets.Senders...)
		} else if target != nil {
			target.routes = []Target{*target}
		}
	}
	if target == nil || target.ID != expected || !validTarget(target) || !s.current(id, protocol, value) {
		return uuid.Nil, Target{}, "", ErrAuthentication
	}
	return id, *target, revision, nil
}

// GeneratePassword is useful to native callers; browser clients use Web Crypto.
func GeneratePassword() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Invalidate closes established sessions when encrypted provider settings change.
// Revision binding prevents the old credential copy being used for new sessions.
func (s *Service) Invalidate(id uuid.UUID) { s.mu.Lock(); defer s.mu.Unlock(); s.closeUserLocked(id) }
