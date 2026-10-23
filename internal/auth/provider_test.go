package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/bytemare/opaque"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/user"
)

func TestOPAQUERegistrationAndLoginRoundTrip(t *testing.T) {
	store, err := filestore.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewManager(30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	users := user.NewService(store, index, sessions, nil)
	setup, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(setup.Value, index, store, users, sessions)
	if err != nil {
		t.Fatal(err)
	}
	const login, password = "alice@example.test", "a sufficiently long password"
	conf := opaque.DefaultConfiguration()
	client, err := conf.Client()
	if err != nil {
		t.Fatal(err)
	}
	request, err := client.RegistrationInit([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	flowID, response, id, err := service.RegistrationStart(login, wire(request.Serialize()))
	if err != nil {
		t.Fatal(err)
	}
	registrationResponse, err := client.Deserialize.RegistrationResponse(mustWire(t, response))
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := client.RegistrationFinalize(registrationResponse, []byte(login), []byte("mail-aggregator"))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]json.RawMessage)
	for _, name := range []string{"config", "accounts", "identities", "smtp", "preferences"} {
		files[name] = json.RawMessage(`{"version":1,"algorithm":"AES-256-GCM","key_version":1,"nonce":"AAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA"}`)
	}
	metadata := json.RawMessage(`{"version":1,"kdf":"argon2id","kdf_salt":"AAAAAAAAAAAAAAAAAAAAAA","kdf_parameters":{"memory_kib":65536,"iterations":3,"parallelism":1,"key_length":32},"wrap_algorithm":"AES-256-GCM","wrapped_vmk":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","wrap_nonce":"AAAAAAAAAAAAAAAA","key_version":1}`)
	registered, err := service.RegistrationFinish(flowID, RegistrationPayload{Record: wire(record.Serialize()), Vault: metadata, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if registered.UserID != id || registered.Token == "" || registered.View.State != session.AuthenticatedLocked {
		t.Fatal("registration did not create a locked authenticated session")
	}

	wrongClient, err := conf.Client()
	if err != nil {
		t.Fatal(err)
	}
	wrongKE1, err := wrongClient.GenerateKE1([]byte("incorrect password"))
	if err != nil {
		t.Fatal(err)
	}
	wrongFlow, wrongResponse, err := service.LoginStart(login, wire(wrongKE1.Serialize()), "", [16]byte{})
	if err != nil {
		t.Fatal(err)
	}
	wrongKE2, err := wrongClient.Deserialize.KE2(mustWire(t, wrongResponse))
	if err != nil {
		t.Fatal(err)
	}
	wrongKE3, _, _, err := wrongClient.GenerateKE3(wrongKE2, []byte(login), []byte("mail-aggregator"))
	if err == nil {
		if _, finishErr := service.LoginFinish(wrongFlow, wire(wrongKE3.Serialize())); finishErr == nil {
			t.Fatal("incorrect password authenticated")
		}
	} else {
		// OPAQUE clients may reject the server's authenticated response before
		// producing KE3 when the supplied password is incorrect.
		if _, finishErr := service.LoginFinish(wrongFlow, ""); finishErr == nil {
			t.Fatal("incorrect password authenticated")
		}
	}

	// Restart the auth service using only the key saved in the data directory.
	restored, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	service, err = NewService(restored.Value, index, store, users, sessions)
	if err != nil {
		t.Fatal(err)
	}
	client, err = conf.Client()
	if err != nil {
		t.Fatal(err)
	}
	ke1, err := client.GenerateKE1([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	flowID, response, err = service.LoginStart(login, wire(ke1.Serialize()), "", [16]byte{})
	if err != nil {
		t.Fatal(err)
	}
	ke2, err := client.Deserialize.KE2(mustWire(t, response))
	if err != nil {
		t.Fatal(err)
	}
	ke3, _, _, err := client.GenerateKE3(ke2, []byte(login), []byte("mail-aggregator"))
	if err != nil {
		t.Fatal(err)
	}
	loggedIn, err := service.LoginFinish(flowID, wire(ke3.Serialize()))
	if err != nil {
		t.Fatal(err)
	}
	if loggedIn.UserID != id || loggedIn.Token == "" || loggedIn.View.State != session.AuthenticatedLocked {
		t.Fatal("login did not create an authenticated locked session")
	}
}

func mustWire(t *testing.T, value string) []byte {
	t.Helper()
	result, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
