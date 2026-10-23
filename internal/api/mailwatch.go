package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type MailWatcher interface {
	Watch(context.Context, imap.Configuration, time.Duration, func(imap.Status))
}
type watchLease struct {
	token      [32]byte
	user       uuid.UUID
	weight     int
	monitoring bool
	seen       time.Time
	cancel     context.CancelFunc
}
type watchRegistry struct {
	mu            sync.Mutex
	streams       map[string]*watchLease
	maxAccounts   int
	reservedSlots int
}

func newWatchRegistry(maxAccounts int) *watchRegistry {
	if maxAccounts <= 0 {
		maxAccounts = 20
	}
	// Reserve slots for interactive operations (reading, folder sync, sent copy).
	// Keep at least 2 slots free, or 20% of max, whichever is larger.
	reserved := maxAccounts / 5
	if reserved < 2 {
		reserved = 2
	}

	return &watchRegistry{streams: make(map[string]*watchLease), maxAccounts: maxAccounts, reservedSlots: reserved}
}

// Interactive work and monitoring have separate budgets. A full set of
// watched mailboxes can never consume the interactive reserve.
func (r *watchRegistry) start(token string, user uuid.UUID, weight int, cancel context.CancelFunc) (string, bool) {
	return r.acquire(token, user, weight, cancel, false)
}
func (r *watchRegistry) startWatch(token string, user uuid.UUID, weight int, cancel context.CancelFunc) (string, bool) {
	return r.acquire(token, user, weight, cancel, true)
}
func (r *watchRegistry) acquire(token string, user uuid.UUID, weight int, cancel context.CancelFunc, monitoring bool) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total, owned := 0, 0
	for _, lease := range r.streams {
		if lease.monitoring == monitoring {
			total += lease.weight
			if lease.user == user {
				owned += lease.weight
			}
		}
	}
	perUser, global := r.reservedSlots, 16
	if monitoring {
		perUser, global = r.maxAccounts, 64
		if perUser > global {
			global = perUser
		}
	}
	if weight <= 0 || total+weight > global || owned+weight > perUser {
		return "", false
	}
	id := uuid.NewString()
	r.streams[id] = &watchLease{token: sha256.Sum256([]byte(token)), user: user, weight: weight, monitoring: monitoring, seen: time.Now(), cancel: cancel}
	return id, true
}
func (r *watchRegistry) touch(token, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.streams[id]
	if s == nil || s.token != sha256.Sum256([]byte(token)) {
		return false
	}
	s.seen = time.Now()
	return true
}
func (r *watchRegistry) alive(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.streams[id]
	return s != nil && time.Since(s.seen) < 45*time.Second
}
func (r *watchRegistry) remove(id string) { r.mu.Lock(); delete(r.streams, id); r.mu.Unlock() }
func (r *watchRegistry) stopSession(token string) {
	hash := sha256.Sum256([]byte(token))
	r.mu.Lock()
	var stops []context.CancelFunc
	for _, s := range r.streams {
		if s.token == hash {
			stops = append(stops, s.cancel)
		}
	}
	r.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

func (r *watchRegistry) stopUser(user uuid.UUID) {
	r.mu.Lock()
	var stops []context.CancelFunc
	for _, lease := range r.streams {
		if lease.user == user {
			stops = append(stops, lease.cancel)
		}
	}
	r.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

func (h *handlers) mailPresence(c *gin.Context) {
	var input struct {
		WatchID string `json:"watch_id" binding:"required,max=64"`
	}
	if c.ShouldBindJSON(&input) != nil || !h.deps.watches.touch(c.GetString(contextToken), input.WatchID) {
		respondError(c, http.StatusNotFound, "WATCH_EXPIRED", "Mailbox monitoring has ended.")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) watchMailboxes(c *gin.Context) {
	var input struct {
		Refresh  int      `json:"refresh_interval_seconds"`
		Accounts []string `json:"accounts"`
	}
	if c.ShouldBindJSON(&input) != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid refresh settings.")
		return
	}
	if input.Refresh == 0 {
		input.Refresh = 30
	}
	// Finish reading the bounded request before removing its streaming deadline.
	if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
		respondError(c, 400, "INVALID_INPUT", "Invalid watch request body.")
		return
	}
	if input.Refresh < 10 || input.Refresh > 300 {
		respondError(c, 400, "INVALID_INPUT", "Choose a refresh interval between 10 and 300 seconds.")
		return
	}
	token := c.GetString(contextToken)
	accounts, smtp, err := h.deps.Sessions.Credentials(token)
	clearCredentialCopies(smtp)
	if err != nil || len(accounts) == 0 {
		clearCredentialCopies(accounts)
		respondError(c, http.StatusLocked, "VAULT_LOCKED", "Activate your receiving identities before monitoring.")
		return
	}
	if input.Accounts != nil {
		wanted := make(map[string]bool)
		for _, account := range input.Accounts {
			wanted[account] = true
		}
		for account, secret := range accounts {
			if !wanted[account] {
				clear(secret.Secret)
				clear(secret.OAuthToken)
				delete(accounts, account)
			}
		}
		if len(accounts) != len(wanted) || len(accounts) == 0 {
			clearCredentialCopies(accounts)
			respondError(c, 423, "RECEIVER_UNAVAILABLE", "Some selected receiving identities are unavailable.")
			return
		}
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	id, ok := h.deps.watches.startWatch(token, mustSession(c).UserID, len(accounts), cancel)
	if !ok {
		clearCredentialCopies(accounts)
		respondError(c, 429, "WATCH_LIMIT", "Too many active mailbox connections. Close another tab and try again.")
		return
	}
	defer h.deps.watches.remove(id)
	if h.deps.Operations != nil {
		defer h.deps.Operations.Track(mustSession(c).UserID, cancel)()
		if !h.deps.Users.IsActive(mustSession(c).UserID) {
			clearCredentialCopies(accounts)
			respondError(c, 401, "SESSION_EXPIRED", "The account is no longer active.")
			return
		}
	}
	// These streams have their own presence/session/credential limits. Remove
	// the ordinary HTTP write deadline so IDLE is not cut off every 30 seconds.
	controller := http.NewResponseController(c.Writer)
	readErr := controller.SetReadDeadline(time.Time{})
	if err := controller.SetWriteDeadline(time.Time{}); err != nil || readErr != nil {
		clearCredentialCopies(accounts)
		respondError(c, 500, "STREAM_UNAVAILABLE", "Streaming is unavailable on this server.")
		return
	}
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-store, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Header("X-Watch-ID", id)
	type event struct {
		Type      string `json:"type"`
		AccountID string `json:"account_id,omitempty"`
		*imap.Status
		CheckedAt time.Time `json:"checked_at"`
	}
	events := make(chan event, 20)
	var workers sync.WaitGroup
	for accountID, credential := range accounts {
		workers.Add(1)
		go func(accountID string, credential session.Credential) {
			defer workers.Done()
			h.deps.Receiver.Watch(ctx, imap.Configuration{Host: credential.Host, Port: credential.Port, Security: credential.Security, Username: credential.Username, Password: credential.Secret}, time.Duration(input.Refresh)*time.Second, func(status imap.Status) {
				select {
				case events <- event{Type: "status", AccountID: accountID, Status: &status, CheckedAt: time.Now().UTC()}:
				case <-ctx.Done():
				}
			})
		}(accountID, credential)
	}
	defer func() { cancel(); workers.Wait(); clearCredentialCopies(accounts) }()
	encoder := json.NewEncoder(c.Writer)
	write := func(value event) bool {
		controller := http.NewResponseController(c.Writer)
		if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
			return false
		}
		if encoder.Encode(value) != nil {
			return false
		}
		if controller.Flush() != nil {
			return false
		}
		return controller.SetWriteDeadline(time.Time{}) == nil
	}
	if !write(event{Type: "ready", CheckedAt: time.Now().UTC()}) {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	guard := time.NewTicker(time.Second)
	defer guard.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-guard.C:
			view, err := h.deps.Sessions.Peek(token)
			if err != nil || view.State != session.AuthenticatedUnlocked || !h.deps.watches.alive(id) {
				return
			}
			for _, credential := range accounts {
				if !time.Now().Before(credential.ExpiresAt) {
					return
				}
			}
		case <-heartbeat.C:
			if !write(event{Type: "heartbeat", CheckedAt: time.Now().UTC()}) {
				return
			}
		case value := <-events:
			if !write(value) {
				return
			}
		}
	}
}
