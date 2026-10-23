package imap

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-sasl"
	"github.com/xdg-go/scram"
)

type testSASLSession struct {
	imapserver.Session
	mechanism string
	logins    *atomic.Int32
}

func (s *testSASLSession) Login(user, password string) error {
	s.logins.Add(1)
	return s.Session.Login(user, password)
}
func (s *testSASLSession) AuthenticateMechanisms() []string {
	if s.mechanism == "" {
		return nil
	}
	return []string{s.mechanism}
}
func (s *testSASLSession) Authenticate(mechanism string) (sasl.Server, error) {
	if mechanism != s.mechanism {
		return nil, errors.New("unsupported SASL")
	}
	var conversation *scram.ServerConversation
	if mechanism == "SCRAM-SHA-256" {
		client, _ := scram.SHA256.NewClient("reader", "test-password", "")
		stored := client.GetStoredCredentials(scram.KeyFactors{Salt: "salt", Iters: 4096})
		server, _ := scram.SHA256.NewServer(func(string) (scram.StoredCredentials, error) { return stored, nil })
		conversation = server.NewConversation()
	}
	return &testSASLExchange{session: s, conversation: conversation}, nil
}

type testSASLExchange struct {
	session      *testSASLSession
	conversation *scram.ServerConversation
	awaitAck     bool
}

func (s *testSASLExchange) Next(response []byte) ([]byte, bool, error) {
	if response == nil {
		return nil, false, nil
	}
	if s.session.mechanism == "XOAUTH2" {
		if string(response) != "user=reader\x01auth=Bearer access-token\x01\x01" {
			return nil, false, errors.New("invalid token")
		}
		return nil, true, s.session.Session.Login("reader", "test-password")
	}
	if s.awaitAck {
		if len(response) != 0 {
			return nil, false, errors.New("unexpected proof acknowledgement")
		}
		return nil, true, s.session.Session.Login("reader", "test-password")
	}
	challenge, err := s.conversation.Step(string(response))
	if err != nil {
		return nil, false, err
	}
	if s.conversation.Done() {
		if !s.conversation.Valid() {
			return nil, false, errors.New("invalid SCRAM proof")
		}
		s.awaitAck = true
	}
	return []byte(challenge), false, nil
}
func TestReceiverSASLUsesOAuthOrExplicitSCRAMWithoutLoginFallback(t *testing.T) {
	for _, mechanism := range []string{"XOAUTH2", "SCRAM-SHA-256", "unsupported"} {
		t.Run(mechanism, func(t *testing.T) {
			var logins atomic.Int32
			receiver, cfg, _, _ := readerFixtureWithSession(t, func(s imapserver.Session) imapserver.Session {
				advertised := mechanism
				if mechanism == "unsupported" {
					advertised = ""
				}
				return &testSASLSession{Session: s, mechanism: advertised, logins: &logins}
			})
			if mechanism == "XOAUTH2" {
				cfg.Password = nil
				cfg.OAuthToken = []byte("access-token")
			} else {
				cfg.SASLMechanism = "SCRAM-SHA-256"
			}
			err := receiver.Test(context.Background(), cfg)
			if mechanism == "unsupported" {
				if err == nil {
					t.Fatal("unsupported SASL silently fell back")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if logins.Load() != 0 {
				t.Fatal("credential downgraded to LOGIN")
			}
		})
	}
}
