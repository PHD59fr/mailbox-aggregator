package mfa

import (
	"bytes"
	"encoding/base32"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	for _, test := range []struct {
		seconds int64
		code    string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"}, {1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		if result := TOTP([]byte("12345678901234567890"), test.seconds/30, 8); result != test.code {
			t.Fatalf("time %d: %s", test.seconds, result)
		}
	}
}
func fixture(t *testing.T) (*Service, uuid.UUID, string) {
	t.Helper()
	store, err := filestore.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err = store.CreateUser(id, map[string][]byte{"auth.json": []byte(`{"version":1}`)}); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewManager(time.Hour, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, sessions, []byte("test server setup"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.now = func() time.Time { return time.Unix(1700000000, 0) }
	token := newSession(t, service, id, true)
	return service, id, token
}
func newSession(t *testing.T, s *Service, id uuid.UUID, unlocked bool) string {
	t.Helper()
	token, _, err := s.sessions.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := s.store.ReadUserFile(id, "auth.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.sessions.BindAuthentication(token, revision); err != nil {
		t.Fatal(err)
	}
	if unlocked {
		if _, err = s.sessions.SetReceivingCredentials(token, map[string]session.Credential{"imap": {Secret: []byte("provider secret")}}, time.Hour, 0); err != nil {
			t.Fatal(err)
		}
	}
	return token
}
func enrolled(t *testing.T) (*Service, uuid.UUID, string, []byte, []string) {
	t.Helper()
	s, id, token := fixture(t)
	encoded, uri, err := s.StartEnrollment(id, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(uri) == 0 {
		t.Fatal("missing provisioning URI")
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.ConfirmEnrollment(id, token, TOTP(secret, s.now().Unix()/30, 6))
	if err != nil {
		t.Fatal(err)
	}
	return s, id, token, secret, codes
}
func TestEnrollmentSealsSecretKeepsCurrentSessionAndRevokesOthers(t *testing.T) {
	s, id, token := fixture(t)
	other := newSession(t, s, id, true)
	encoded, _, err := s.StartEnrollment(id, token)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	codes, err := s.ConfirmEnrollment(id, token, TOTP(secret, s.now().Unix()/30, 6))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatal("missing recovery codes")
	}
	if _, err = s.sessions.Peek(other); err == nil {
		t.Fatal("another session survived enrollment")
	}
	view, err := s.sessions.Peek(token)
	if err != nil || view.State != session.AuthenticatedUnlocked || view.MFARevision == "" {
		t.Fatalf("current session lost: %#v %v", view, err)
	}
	data, _, err := s.store.ReadUserFile(id, "mfa.json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, secret) || bytes.Contains(data, []byte(encoded)) || bytes.Contains(data, []byte(codes[0])) {
		t.Fatal("MFA material persisted in plaintext")
	}
	if err = ValidateBackup([]byte("test server setup"), id, data); err != nil {
		t.Fatal(err)
	}
	if err = ValidateBackup([]byte("wrong server setup"), id, data); err == nil {
		t.Fatal("wrong sealing key accepted")
	}
	if err = ValidateBackup([]byte("test server setup"), uuid.New(), data); err == nil {
		t.Fatal("MFA record was portable across users")
	}
}
func TestLoginRequiresUnusedCodeAndRejectsReplayedOrExpiredFlows(t *testing.T) {
	s, id, _, secret, _ := enrolled(t)
	token := newSession(t, s, id, false)
	flow, required, err := s.BeginLogin(id, token)
	if err != nil || !required {
		t.Fatal("second factor not requested")
	}
	if _, _, err = s.VerifyLogin(flow, TOTP(secret, s.now().Unix()/30, 6)); !errors.Is(err, ErrVerification) {
		t.Fatal("enrollment code replayed")
	}
	now := s.now().Add(30 * time.Second)
	s.now = func() time.Time { return now }
	issued, view, err := s.VerifyLogin(flow, TOTP(secret, now.Unix()/30, 6))
	if err != nil || issued != token || view.MFARevision == "" {
		t.Fatalf("valid factor rejected: %v", err)
	}
	if _, _, err = s.VerifyLogin(flow, TOTP(secret, now.Unix()/30, 6)); !errors.Is(err, ErrVerification) {
		t.Fatal("login flow reused")
	}
	expired := newSession(t, s, id, false)
	old, _, _ := s.BeginLogin(id, expired)
	s.now = func() time.Time { return now.Add(3 * time.Minute) }
	if _, _, err = s.VerifyLogin(old, "000000"); !errors.Is(err, ErrVerification) {
		t.Fatal("expired flow accepted")
	}
	if _, err = s.sessions.Peek(expired); err == nil {
		t.Fatal("expired pending session retained")
	}
}
func TestRecoveryCodeIsConsumedAtomicallyAndDisablingDoesNotRecoverVault(t *testing.T) {
	s, id, current, _, codes := enrolled(t)
	first := newSession(t, s, id, false)
	second := newSession(t, s, id, false)
	a, _, _ := s.BeginLogin(id, first)
	b, _, _ := s.BeginLogin(id, second)
	var wg sync.WaitGroup
	outcomes := make(chan bool, 2)
	for _, flow := range []string{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, err := s.VerifyLogin(flow, codes[0]); outcomes <- err == nil }()
	}
	wg.Wait()
	close(outcomes)
	succeeded := 0
	for ok := range outcomes {
		if ok {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("one-use recovery code accepted %d times", succeeded)
	}
	_, remaining, _, err := s.Status(id)
	if err != nil || remaining != 9 {
		t.Fatalf("wrong recovery count: %d %v", remaining, err)
	}
	if _, err = s.Change(id, current, codes[1], true); err != nil {
		t.Fatal(err)
	}
	enabled, remaining, binding, err := s.Status(id)
	if err != nil || enabled || remaining != 0 || binding != "" {
		t.Fatal("second factor not disabled")
	}
	view, err := s.sessions.Peek(current)
	if err != nil || view.MFARevision != "" {
		t.Fatal("current session not preserved")
	}
}
func TestCorruptMFAStateFailsClosed(t *testing.T) {
	s, id, _, _, _ := enrolled(t)
	_, revision, _ := s.store.ReadUserFile(id, "mfa.json")
	if _, err := s.store.WriteUserFile(id, "mfa.json", []byte(`{"version":1}`), revision); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Status(id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("corrupt state treated as disabled")
	}
}
