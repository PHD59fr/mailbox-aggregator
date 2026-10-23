package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound        = errors.New("session not found")
	ErrExpired         = errors.New("session expired")
	ErrLockRevision    = errors.New("session lock revision changed")
	ErrCredentialLimit = errors.New("too many active credentials")
)

type State string

const (
	AuthenticatedLocked   State = "AUTHENTICATED_LOCKED"
	AuthenticatedUnlocked State = "AUTHENTICATED_UNLOCKED"
)

type View struct {
	AuthRevision       string    `json:"-"`
	MFARevision        string    `json:"-"`
	UserID             uuid.UUID `json:"user_id"`
	State              State     `json:"state"`
	CreatedAt          time.Time `json:"created_at"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	ReauthenticatedAt  time.Time `json:"reauthenticated_at,omitempty"`
	CSRFToken          string    `json:"csrf_token"`
	LockRevision       uint64    `json:"lock_revision"`
	IdleTimeoutSeconds int       `json:"idle_timeout_seconds"`
	MaxAccounts        int       `json:"max_accounts"`
}

type Credential struct {
	Host          string    `json:"host"`
	Port          uint16    `json:"port"`
	Security      string    `json:"security"`
	Username      string    `json:"username"`
	SenderName    string    `json:"sender_name,omitempty"`
	ReplyTo       string    `json:"reply_to,omitempty"`
	SenderEmail   string    `json:"sender_email,omitempty"`
	Secret        []byte    `json:"secret"`
	SASLMechanism string    `json:"sasl_mechanism,omitempty"`
	OAuthToken    []byte    `json:"oauth_token,omitempty"`
	ActivatedAt   time.Time `json:"activated_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type credentialSet struct {
	Accounts map[string]*Credential
	SMTP     map[string]*Credential
}
type record struct {
	View           View
	absoluteExpiry time.Time
	credentials    credentialSet
}

type Manager struct {
	mu          sync.Mutex
	sessions    map[[32]byte]*record
	idle        time.Duration
	absolute    time.Duration
	maxAccounts int
	now         func() time.Time
}

func NewManager(idle, absolute time.Duration) (*Manager, error) {
	if idle <= 0 || absolute <= 0 || idle > absolute {
		return nil, errors.New("invalid session expiry configuration")
	}
	return &Manager{sessions: make(map[[32]byte]*record), idle: idle, absolute: absolute, maxAccounts: 20, now: time.Now}, nil
}

func (m *Manager) Create(userID uuid.UUID) (string, View, error) {
	if userID == uuid.Nil {
		return "", View{}, errors.New("invalid user ID")
	}
	token, err := randomToken(32)
	if err != nil {
		return "", View{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return "", View{}, err
	}
	now := m.now().UTC()
	key := tokenHash(token)
	view := View{UserID: userID, State: AuthenticatedLocked, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(m.idle), ReauthenticatedAt: now, CSRFToken: csrf, IdleTimeoutSeconds: int(m.idle.Seconds()), MaxAccounts: m.maxAccounts}
	m.mu.Lock()
	m.sessions[key] = &record{View: view, absoluteExpiry: now.Add(m.absolute)}
	m.mu.Unlock()
	return token, view, nil
}

func (m *Manager) BindAuthentication(token, revision string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.recordLocked(tokenHash(token), m.now().UTC())
	if err != nil {
		return err
	}
	rec.View.AuthRevision = revision
	return nil
}

func (m *Manager) Get(token string) (View, error) {
	return m.get(token, true)
}

// Peek checks a long-running operation without extending the idle deadline.
func (m *Manager) Peek(token string) (View, error) { return m.get(token, false) }

func (m *Manager) get(token string, touch bool) (View, error) {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	rec, err := m.recordLocked(key, now)
	if err != nil {
		return View{}, err
	}
	expireCredentials(rec, now)
	if touch {
		rec.View.LastSeenAt = now
		rec.View.ExpiresAt = minTime(now.Add(m.idle), rec.absoluteExpiry)
	}
	rec.View.IdleTimeoutSeconds = int(m.idle.Seconds())
	rec.View.MaxAccounts = m.maxAccounts
	return rec.View, nil
}

func (m *Manager) ValidateCSRF(token, csrf string) bool {
	key := tokenHash(token)
	left := sha256.Sum256([]byte(csrf))
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.recordLocked(key, m.now().UTC())
	if err != nil {
		return false
	}
	right := sha256.Sum256([]byte(rec.View.CSRFToken))
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}

func (m *Manager) SetCredentials(token string, accounts, smtp map[string]Credential, lifetime time.Duration, revision uint64) (View, error) {
	return m.setCredentials(token, accounts, smtp, lifetime, revision, false, false, false)
}

// SetSMTPCredentials activates sending independently of active receiving credentials.
func (m *Manager) SetSMTPCredentials(token string, smtp map[string]Credential, lifetime time.Duration, revision uint64) (View, error) {
	return m.setCredentials(token, nil, smtp, lifetime, revision, false, true, false)
}

// SetReceivingCredentials preserves independently activated SMTP credentials.
func (m *Manager) SetReceivingCredentials(token string, accounts map[string]Credential, lifetime time.Duration, revision uint64) (View, error) {
	return m.setCredentials(token, accounts, nil, lifetime, revision, true, false, false)
}

// SetAccountLimit is configured before serving requests. Limits also apply to
// the union of concurrent per-account activations, under the session lock.
func (m *Manager) SetAccountLimit(limit int) {
	if limit <= 0 {
		limit = 20
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxAccounts = limit
}

// MergeCredentials refreshes only supplied accounts. DELETE credentials is the
// explicit revocation operation; it also invalidates pending activations.
func (m *Manager) MergeCredentials(token string, accounts, smtp map[string]Credential, lifetime time.Duration, revision uint64) (View, error) {
	return m.setCredentials(token, accounts, smtp, lifetime, revision, false, false, true)
}
func mergedCount(current map[string]*Credential, next map[string]Credential) int {
	count := len(current)
	for id := range next {
		if current[id] == nil {
			count++
		}
	}
	return count
}
func mergeCredentials(current map[string]*Credential, next map[string]Credential, now time.Time, lifetime time.Duration) map[string]*Credential {
	if current == nil {
		current = make(map[string]*Credential)
	}
	for id, value := range cloneCredentials(next, now, lifetime) {
		if old := current[id]; old != nil {
			clear(old.Secret)
			clear(old.OAuthToken)
		}
		current[id] = value
	}
	return current
}

func (m *Manager) setCredentials(token string, accounts, smtp map[string]Credential, lifetime time.Duration, revision uint64, accountsOnly, smtpOnly, merge bool) (View, error) {
	if lifetime <= 0 || lifetime > m.idle {
		return View{}, errors.New("invalid credential lifetime")
	}
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	rec, err := m.recordLocked(key, now)
	if err != nil {
		return View{}, err
	}
	if rec.View.LockRevision != revision {
		return View{}, ErrLockRevision
	}
	expireCredentials(rec, now)
	lifetime = minTime(now.Add(lifetime), rec.absoluteExpiry).Sub(now)
	if merge {
		if mergedCount(rec.credentials.Accounts, accounts) > m.maxAccounts || mergedCount(rec.credentials.SMTP, smtp) > m.maxAccounts {
			return View{}, ErrCredentialLimit
		}
		rec.credentials.Accounts = mergeCredentials(rec.credentials.Accounts, accounts, now, lifetime)
		rec.credentials.SMTP = mergeCredentials(rec.credentials.SMTP, smtp, now, lifetime)
		rec.View.State = AuthenticatedUnlocked
		return rec.View, nil
	}
	if len(accounts) > m.maxAccounts || len(smtp) > m.maxAccounts {
		return View{}, ErrCredentialLimit
	}
	if !smtpOnly {
		clearCredentials(&credentialSet{Accounts: rec.credentials.Accounts})
		rec.credentials.Accounts = cloneCredentials(accounts, now, lifetime)
	}
	if !accountsOnly {
		clearCredentials(&credentialSet{SMTP: rec.credentials.SMTP})
		rec.credentials.SMTP = cloneCredentials(smtp, now, lifetime)
	}
	rec.View.State = AuthenticatedUnlocked
	return rec.View, nil
}

func (m *Manager) Credentials(token string) (map[string]Credential, map[string]Credential, error) {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	rec, err := m.recordLocked(key, now)
	if err != nil {
		return nil, nil, err
	}
	expireCredentials(rec, now)
	if rec.View.State != AuthenticatedUnlocked {
		return nil, nil, errors.New("vault locked")
	}
	accounts, okA := activeCredentials(rec.credentials.Accounts, now)
	smtp, okS := activeCredentials(rec.credentials.SMTP, now)
	if !okA && !okS {
		clearCredentials(&rec.credentials)
		rec.View.State = AuthenticatedLocked
		return nil, nil, errors.New("credentials expired")
	}
	return accounts, smtp, nil
}

func (m *Manager) Lock(token string) (View, error) {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.recordLocked(key, m.now().UTC())
	if err != nil {
		return View{}, err
	}
	clearCredentials(&rec.credentials)
	rec.View.LockRevision++
	rec.View.State = AuthenticatedLocked
	rec.View.IdleTimeoutSeconds = int(m.idle.Seconds())
	rec.View.MaxAccounts = m.maxAccounts
	return rec.View, nil
}
func (m *Manager) Reauthenticate(token string) error {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	rec, err := m.recordLocked(key, now)
	if err != nil {
		return err
	}
	rec.View.ReauthenticatedAt = now
	return nil
}
func (m *Manager) IsFresh(token string, maximumAge time.Duration) bool {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	rec, err := m.recordLocked(key, now)
	return err == nil && now.Sub(rec.View.ReauthenticatedAt) <= maximumAge
}

// recordLocked validates authentication expiry even for callers other than Get.
// The caller must hold m.mu.
func (m *Manager) recordLocked(key [32]byte, now time.Time) (*record, error) {
	rec := m.sessions[key]
	if rec == nil {
		return nil, ErrNotFound
	}
	if !now.Before(rec.View.ExpiresAt) || !now.Before(rec.absoluteExpiry) {
		clearCredentials(&rec.credentials)
		delete(m.sessions, key)
		return nil, ErrExpired
	}
	return rec, nil
}

// PurgeExpired erases abandoned sessions and each expired provider secret,
// including when another credential group still has a valid lease.
func (m *Manager) PurgeExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	for key := range m.sessions {
		if rec, err := m.recordLocked(key, now); err == nil {
			expireCredentials(rec, now)
		}
	}
}

// RunCleanup bounds retention without requiring another request from the client.
// It runs until the server's shutdown context is cancelled.
func (m *Manager) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.PurgeExpired()
		}
	}
}

func expireCredentials(rec *record, now time.Time) {
	for _, group := range []map[string]*Credential{rec.credentials.Accounts, rec.credentials.SMTP} {
		for id, value := range group {
			if !now.Before(value.ExpiresAt) {
				clear(value.Secret)
				clear(value.OAuthToken)
				delete(group, id)
			}
		}
	}
	if len(rec.credentials.Accounts) == 0 && len(rec.credentials.SMTP) == 0 {
		rec.View.State = AuthenticatedLocked
	}
}
func (m *Manager) Revoke(token string) {
	key := tokenHash(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec := m.sessions[key]; rec != nil {
		clearCredentials(&rec.credentials)
		delete(m.sessions, key)
	}
}
func (m *Manager) RevokeUser(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, rec := range m.sessions {
		if rec.View.UserID == id {
			clearCredentials(&rec.credentials)
			delete(m.sessions, key)
		}
	}
}
func (m *Manager) BindMFA(token, revision string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.recordLocked(tokenHash(token), m.now().UTC())
	if err != nil {
		return err
	}
	record.View.MFARevision = revision
	return nil
}
func (m *Manager) RevokeOthers(id uuid.UUID, keep string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	retained := tokenHash(keep)
	for key, rec := range m.sessions {
		if rec.View.UserID == id && key != retained {
			clearCredentials(&rec.credentials)
			delete(m.sessions, key)
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, rec := range m.sessions {
		clearCredentials(&rec.credentials)
		delete(m.sessions, key)
	}
}
func (m *Manager) Count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.sessions) }

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func tokenHash(token string) [32]byte { return sha256.Sum256([]byte(token)) }
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func cloneCredentials(source map[string]Credential, now time.Time, lifetime time.Duration) map[string]*Credential {
	out := make(map[string]*Credential, len(source))
	for id, value := range source {
		copyValue := value
		copyValue.Secret = append([]byte(nil), value.Secret...)
		copyValue.OAuthToken = append([]byte(nil), value.OAuthToken...)
		copyValue.ActivatedAt = now
		copyValue.ExpiresAt = now.Add(lifetime)
		out[id] = &copyValue
	}
	return out
}
func activeCredentials(source map[string]*Credential, now time.Time) (map[string]Credential, bool) {
	out := make(map[string]Credential)
	for id, value := range source {
		if now.Before(value.ExpiresAt) {
			copyValue := *value
			copyValue.Secret = append([]byte(nil), value.Secret...)
			copyValue.OAuthToken = append([]byte(nil), value.OAuthToken...)
			out[id] = copyValue
		}
	}
	return out, len(out) > 0
}
func clearCredentials(set *credentialSet) {
	for _, group := range []map[string]*Credential{set.Accounts, set.SMTP} {
		for id, value := range group {
			for i := range value.Secret {
				value.Secret[i] = 0
			}
			for i := range value.OAuthToken {
				value.OAuthToken[i] = 0
			}
			delete(group, id)
		}
	}
	set.Accounts = nil
	set.SMTP = nil
}
