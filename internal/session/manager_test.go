package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCredentialActivationRequiresCurrentLockRevision(t *testing.T) {
	for _, group := range []string{"both", "accounts", "smtp"} {
		t.Run(group, func(t *testing.T) {
			manager, _ := NewManager(30*time.Minute, time.Hour)
			token, _, _ := manager.Create(uuid.New())
			credentials := map[string]Credential{"own": {Secret: []byte("provider-secret")}}
			activate := func(revision uint64) (View, error) {
				switch group {
				case "accounts":
					return manager.SetReceivingCredentials(token, credentials, time.Minute, revision)
				case "smtp":
					return manager.SetSMTPCredentials(token, credentials, time.Minute, revision)
				default:
					return manager.SetCredentials(token, credentials, credentials, time.Minute, revision)
				}
			}
			if _, err := activate(0); err != nil {
				t.Fatal(err)
			}
			locked, err := manager.Lock(token)
			if err != nil || locked.LockRevision != 1 {
				t.Fatalf("lock: %#v %v", locked, err)
			}
			if _, err = activate(0); !errors.Is(err, ErrLockRevision) {
				t.Fatalf("stale activation accepted: %v", err)
			}
			rec := manager.sessions[tokenHash(token)]
			if rec.View.State != AuthenticatedLocked || len(rec.credentials.Accounts)+len(rec.credentials.SMTP) != 0 {
				t.Fatal("stale activation restored provider secrets")
			}
			if _, err = activate(locked.LockRevision); err != nil {
				t.Fatalf("fresh activation rejected: %v", err)
			}
			if _, err = activate(0); !errors.Is(err, ErrLockRevision) {
				t.Fatal("an older tab could replace freshly activated credentials")
			}
		})
	}
}

func TestPurgeErasesExpiredSecretsIndependentlyAndPreservesResumeRevision(t *testing.T) {
	manager, _ := NewManager(30*time.Minute, time.Hour)
	now := time.Now()
	manager.now = func() time.Time { return now }
	token, _, _ := manager.Create(uuid.New())
	_, _ = manager.SetReceivingCredentials(token, map[string]Credential{"short": {Secret: []byte("imap-secret"), OAuthToken: []byte("oauth-secret")}}, time.Minute, 0)
	_, _ = manager.SetSMTPCredentials(token, map[string]Credential{"long": {Secret: []byte("smtp-secret")}}, 10*time.Minute, 0)
	rec := manager.sessions[tokenHash(token)]
	short := rec.credentials.Accounts["short"]
	long := rec.credentials.SMTP["long"]
	now = now.Add(2 * time.Minute)
	manager.PurgeExpired()
	if len(rec.credentials.Accounts) != 0 || string(long.Secret) != "smtp-secret" || rec.View.State != AuthenticatedUnlocked {
		t.Fatal("purge failed to preserve only the independently active SMTP lease")
	}
	if !allZero(short.Secret) || !allZero(short.OAuthToken) {
		t.Fatal("expired provider secret buffers were not erased")
	}
	now = now.Add(9 * time.Minute)
	manager.PurgeExpired()
	if !allZero(long.Secret) || rec.View.State != AuthenticatedLocked || rec.View.LockRevision != 0 || manager.Count() != 1 {
		t.Fatal("lease expiry did not erase secrets or invalidated the authenticated tab resume")
	}
	if _, err := manager.SetReceivingCredentials(token, map[string]Credential{"renewed": {Secret: []byte("new")}}, time.Minute, 0); err != nil {
		t.Fatalf("lease expiry prevented renewal: %v", err)
	}
	now = now.Add(20 * time.Minute)
	secret := rec.credentials.Accounts["renewed"].Secret
	manager.PurgeExpired()
	if manager.Count() != 0 || !allZero(secret) {
		t.Fatal("abandoned authentication session was retained after idle expiry")
	}
}

func TestExpiredAuthenticationCannotBeReactivatedOrReauthenticated(t *testing.T) {
	for _, operation := range []string{"both", "accounts", "smtp", "credentials", "reauth", "csrf", "fresh", "lock"} {
		t.Run(operation, func(t *testing.T) {
			manager, _ := NewManager(time.Minute, time.Hour)
			now := time.Now()
			manager.now = func() time.Time { return now }
			token, view, _ := manager.Create(uuid.New())
			_, _ = manager.SetCredentials(token, map[string]Credential{"own": {Secret: []byte("secret")}}, nil, time.Minute, 0)
			secret := manager.sessions[tokenHash(token)].credentials.Accounts["own"].Secret
			now = now.Add(time.Minute)
			var err error
			switch operation {
			case "both":
				_, err = manager.SetCredentials(token, nil, nil, time.Minute, 0)
			case "accounts":
				_, err = manager.SetReceivingCredentials(token, nil, time.Minute, 0)
			case "smtp":
				_, err = manager.SetSMTPCredentials(token, nil, time.Minute, 0)
			case "credentials":
				_, _, err = manager.Credentials(token)
			case "reauth":
				err = manager.Reauthenticate(token)
			case "lock":
				_, err = manager.Lock(token)
			case "csrf":
				if manager.ValidateCSRF(token, view.CSRFToken) {
					t.Fatal("expired CSRF accepted")
				}
			case "fresh":
				if manager.IsFresh(token, time.Hour) {
					t.Fatal("expired session considered fresh")
				}
			}
			if operation != "csrf" && operation != "fresh" && !errors.Is(err, ErrExpired) {
				t.Fatalf("expiry not enforced: %v", err)
			}
			if manager.Count() != 0 || !allZero(secret) {
				t.Fatal("expired record or secrets remain")
			}
		})
	}
}

func TestCleanupRunsWithoutClientTrafficAndStopsOnShutdown(t *testing.T) {
	manager, _ := NewManager(time.Minute, time.Hour)
	token, _, _ := manager.Create(uuid.New())
	_, _ = manager.SetReceivingCredentials(token, map[string]Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	rec := manager.sessions[tokenHash(token)]
	secret := rec.credentials.Accounts["own"].Secret
	rec.View.ExpiresAt = time.Now().Add(-time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); manager.RunCleanup(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for manager.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if manager.Count() != 0 || !allZero(secret) {
		t.Fatal("background cleanup did not erase the abandoned session")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop on shutdown")
	}
}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

func TestSessionExpiryLockAndRevocation(t *testing.T) {
	manager, err := NewManager(30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	id := uuid.New()
	token, view, err := manager.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != AuthenticatedLocked || !manager.ValidateCSRF(token, view.CSRFToken) {
		t.Fatal("invalid initial session")
	}
	secret := []byte("secret")
	view, err = manager.SetCredentials(token, map[string]Credential{"account": {Username: "me", Secret: secret}}, nil, 10*time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != AuthenticatedUnlocked {
		t.Fatal("session did not unlock")
	}
	accounts, _, err := manager.Credentials(token)
	if err != nil || string(accounts["account"].Secret) != "secret" {
		t.Fatal("credential unavailable")
	}
	manager.RevokeUser(id)
	if _, err = manager.Get(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session accepted: %v", err)
	}
	for _, value := range secret {
		if value != 0 {
			return
		}
	}
	t.Fatal("caller-owned secret was unexpectedly modified")
}

func TestExplicitLockRevisionCannotBeConfusedWithCredentialLeaseExpiry(t *testing.T) {
	manager, _ := NewManager(30*time.Minute, time.Hour)
	now := time.Now()
	manager.now = func() time.Time { return now }
	token, view, _ := manager.Create(uuid.New())
	if view.LockRevision != 0 {
		t.Fatal("initial revision changed")
	}
	_, _ = manager.SetReceivingCredentials(token, map[string]Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	now = now.Add(2 * time.Minute)
	_, _, _ = manager.Credentials(token)
	view, _ = manager.Get(token)
	if view.State != AuthenticatedLocked || view.LockRevision != 0 {
		t.Fatal("credential expiry invalidated an otherwise valid tab resume")
	}
	if _, err := manager.Lock(token); err != nil {
		t.Fatal(err)
	}
	view, _ = manager.Get(token)
	if view.LockRevision != 1 {
		t.Fatal("explicit lock did not invalidate old resume records")
	}
	view, _ = manager.SetReceivingCredentials(token, map[string]Credential{"own": {Secret: []byte("new-secret")}}, time.Minute, 1)
	if view.LockRevision != 1 {
		t.Fatal("credential activation reset the lock revision")
	}
}

func TestIdleAndAbsoluteExpiry(t *testing.T) {
	manager, _ := NewManager(10*time.Minute, 20*time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	token, _, _ := manager.Create(uuid.New())
	now = now.Add(9 * time.Minute)
	if _, err := manager.Get(token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Minute)
	if _, err := manager.Get(token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	if _, err := manager.Get(token); !errors.Is(err, ErrExpired) {
		t.Fatalf("absolute expiry not enforced: %v", err)
	}
}

func TestPeekDoesNotKeepSessionAlive(t *testing.T) {
	manager, _ := NewManager(10*time.Minute, time.Hour)
	now := time.Now()
	manager.now = func() time.Time { return now }
	token, initial, _ := manager.Create(uuid.New())
	now = now.Add(9 * time.Minute)
	view, err := manager.Peek(token)
	if err != nil {
		t.Fatal(err)
	}
	if !view.ExpiresAt.Equal(initial.ExpiresAt) {
		t.Fatal("Peek extended the session")
	}
	now = now.Add(2 * time.Minute)
	if _, err = manager.Peek(token); !errors.Is(err, ErrExpired) {
		t.Fatal("expired stream session remained valid")
	}
}

func TestIndependentReceivingActivation(t *testing.T) {
	manager, _ := NewManager(30*time.Minute, time.Hour)
	token, _, _ := manager.Create(uuid.New())
	_, _ = manager.SetSMTPCredentials(token, map[string]Credential{"sender": {Secret: []byte("smtp-secret")}}, time.Minute, 0)
	_, err := manager.SetReceivingCredentials(token, map[string]Credential{"receiver": {Secret: []byte("imap-secret")}}, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	accounts, smtp, err := manager.Credentials(token)
	if err != nil || string(accounts["receiver"].Secret) != "imap-secret" || string(smtp["sender"].Secret) != "smtp-secret" {
		t.Fatal("receiving activation replaced sending credentials")
	}
	for _, record := range accounts {
		clear(record.Secret)
	}
	for _, record := range smtp {
		clear(record.Secret)
	}
}

func TestConcurrentTargetedCredentialsPreserveOtherAccountsAndBoundUnion(t *testing.T) {
	manager, _ := NewManager(time.Minute, time.Hour)
	manager.SetAccountLimit(2)
	token, _, _ := manager.Create(uuid.New())
	if _, err := manager.MergeCredentials(token, map[string]Credential{"a": {Secret: []byte("A")}}, nil, time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.MergeCredentials(token, map[string]Credential{"b": {Secret: []byte("B")}}, nil, time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	accounts, _, err := manager.Credentials(token)
	if err != nil || len(accounts) != 2 {
		t.Fatal("targeted activation removed another account")
	}
	for _, value := range accounts {
		clear(value.Secret)
	}
	if _, err := manager.MergeCredentials(token, map[string]Credential{"c": {Secret: []byte("C")}}, nil, time.Minute, 0); !errors.Is(err, ErrCredentialLimit) {
		t.Fatal("merged limit not enforced")
	}
	if _, err := manager.MergeCredentials(token, map[string]Credential{"a": {Secret: []byte("refreshed")}}, nil, time.Minute, 0); err != nil {
		t.Fatal("refresh at limit refused")
	}
	_, _ = manager.Lock(token)
	if _, err := manager.MergeCredentials(token, map[string]Credential{"a": {Secret: []byte("stale")}}, nil, time.Minute, 0); !errors.Is(err, ErrLockRevision) {
		t.Fatal("merge bypassed lock revision")
	}
}

func TestSimultaneousMergesCannotExceedCredentialLimit(t *testing.T) {
	manager, _ := NewManager(time.Minute, time.Hour)
	manager.SetAccountLimit(2)
	token, _, _ := manager.Create(uuid.New())
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			_, err := manager.MergeCredentials(token, map[string]Credential{fmt.Sprint(id): {Secret: []byte("test")}}, nil, time.Minute, 0)
			results <- err
		}(i)
	}
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrCredentialLimit) {
			t.Fatal(err)
		}
	}
	if success != 2 {
		t.Fatalf("got %d successful activations, want 2", success)
	}
	manager.Close()
}
