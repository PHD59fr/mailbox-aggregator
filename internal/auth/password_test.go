package auth

import (
	"encoding/json"
	"errors"
	"github.com/bytemare/opaque"
	"github.com/example/mail-aggregator/internal/diagnostic"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/google/uuid"
	"testing"
	"time"
)

const passwordTestLogin = "password@example.test"
const passwordTestSecret = "an old sufficiently long password"

func passwordFixture(t *testing.T) (*Service, Result, RegistrationPayload) {
	t.Helper()
	store, err := filestore.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := session.NewManager(30*time.Minute, time.Hour)
	users := user.NewService(store, index, sessions, nil)
	setup, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(setup.Value, index, store, users, sessions)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := opaque.DefaultConfiguration().Client()
	request, err := client.RegistrationInit([]byte(passwordTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	flow, response, _, err := service.RegistrationStart(passwordTestLogin, wire(request.Serialize()))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := client.Deserialize.RegistrationResponse(mustWire(t, response))
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := client.RegistrationFinalize(parsed, []byte(passwordTestLogin), []byte("mail-aggregator"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]json.RawMessage{}
	for _, name := range []string{"config", "accounts", "identities", "smtp", "preferences"} {
		files[name] = json.RawMessage(`{"version":1,"algorithm":"AES-256-GCM","key_version":1,"nonce":"AAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA"}`)
	}
	metadata := json.RawMessage(`{"version":1,"kdf":"argon2id","kdf_salt":"AAAAAAAAAAAAAAAAAAAAAA","kdf_parameters":{"memory_kib":65536,"iterations":3,"parallelism":1,"key_length":32},"wrap_algorithm":"AES-256-GCM","wrapped_vmk":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","wrap_nonce":"AAAAAAAAAAAAAAAA","key_version":1}`)
	payload := RegistrationPayload{Record: wire(record.Serialize()), Vault: metadata, Files: files}
	registered, err := service.RegistrationFinish(flow, payload)
	if err != nil {
		t.Fatal(err)
	}
	return service, registered, payload
}
func beginPasswordLogin(t *testing.T, s *Service, password, token string, id uuid.UUID) (string, string, error) {
	t.Helper()
	client, err := opaque.DefaultConfiguration().Client()
	if err != nil {
		return "", "", err
	}
	ke1, err := client.GenerateKE1([]byte(password))
	if err != nil {
		return "", "", err
	}
	flow, response, err := s.LoginStart(passwordTestLogin, wire(ke1.Serialize()), token, id)
	if err != nil {
		return "", "", err
	}
	ke2, err := client.Deserialize.KE2(mustWire(t, response))
	if err != nil {
		return "", "", err
	}
	ke3, _, _, err := client.GenerateKE3(ke2, []byte(passwordTestLogin), []byte("mail-aggregator"))
	if err != nil {
		return "", "", err
	}
	return flow, wire(ke3.Serialize()), nil
}
func TestPasswordChangeRequiresOneUseReauthRevokesOldFlows(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(map[bool]string{false: "rewrap", true: "rotate"}[rotate], func(t *testing.T) {
			s, registered, payload := passwordFixture(t)
			client, _ := opaque.DefaultConfiguration().Client()
			request, _ := client.RegistrationInit([]byte("new sufficiently long password"))
			if _, _, err := s.PasswordStart(passwordTestLogin, wire(request.Serialize()), registered.Token, registered.UserID, rotate); err == nil {
				t.Fatal("login alone authorized credential replacement")
			}
			pending, ke3, err := beginPasswordLogin(t, s, passwordTestSecret, "", uuid.Nil)
			if err != nil {
				t.Fatal(err)
			}
			flow, response, err := beginPasswordLogin(t, s, passwordTestSecret, registered.Token, registered.UserID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.LoginFinish(flow, response); err != nil {
				t.Fatal(err)
			}
			change, response, err := s.PasswordStart(passwordTestLogin, wire(request.Serialize()), registered.Token, registered.UserID, rotate)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.PasswordStart(passwordTestLogin, wire(request.Serialize()), registered.Token, registered.UserID, rotate); err == nil {
				t.Fatal("reauthentication grant reused")
			}
			parsed, _ := client.Deserialize.RegistrationResponse(mustWire(t, response))
			record, _, err := client.RegistrationFinalize(parsed, []byte(passwordTestLogin), []byte("mail-aggregator"))
			if err != nil {
				t.Fatal(err)
			}
			next := RegistrationPayload{Record: wire(record.Serialize()), Vault: payload.Vault}
			if rotate {
				var metadata map[string]any
				_ = json.Unmarshal(payload.Vault, &metadata)
				metadata["key_version"] = 2
				next.Vault, _ = json.Marshal(metadata)
				next.Files = map[string]json.RawMessage{}
				for name, data := range payload.Files {
					var envelope map[string]any
					_ = json.Unmarshal(data, &envelope)
					envelope["key_version"] = 2
					next.Files[name], _ = json.Marshal(envelope)
				}
			}
			oldAuth := registered.View.AuthRevision
			if err := s.PasswordFinish(change, registered.Token, next); err != nil {
				t.Fatal(err)
			}
			if _, err := s.sessions.Peek(registered.Token); err == nil {
				t.Fatal("old session survived")
			}
			if _, err := s.LoginFinish(pending, ke3); err == nil {
				t.Fatal("pending login survived password change")
			}
			if _, _, err := beginPasswordLogin(t, s, passwordTestSecret, "", uuid.Nil); err == nil {
				t.Fatal("old password authenticated")
			}
			flow, response, err = beginPasswordLogin(t, s, "new sufficiently long password", "", uuid.Nil)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := s.LoginFinish(flow, response)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.View.AuthRevision == oldAuth {
				t.Fatal("authentication generation did not change")
			}
			_, revision, _ := s.store.ReadUserFile(registered.UserID, "preferences.enc")
			if _, err := s.store.WriteUserFile(registered.UserID, "preferences.enc", payload.Files["preferences"], revision, oldAuth); !errors.Is(err, filestore.ErrConflict) {
				t.Fatal("old pending mutation wrote after key rotation")
			}
			report, err := diagnostic.Check(s.store.Root(), "", 1<<20)
			if err != nil || report.Users != 1 {
				t.Fatalf("read-only backup validation: %v", err)
			}
		})
	}
}
