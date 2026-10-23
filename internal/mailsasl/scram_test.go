package mailsasl

import (
	"github.com/xdg-go/scram"
	"strings"
	"testing"
)

func TestSCRAM256VerifiesServer(t *testing.T) {
	credentials, _ := scram.SHA256.NewClient("alice", "secret", "")
	stored := credentials.GetStoredCredentials(scram.KeyFactors{Salt: "salt", Iters: 4096})
	server, _ := scram.SHA256.NewServer(func(user string) (scram.StoredCredentials, error) {
		if user != "alice" {
			t.Errorf("unexpected user %s", user)
		}
		return stored, nil
	})
	for _, bad := range []bool{false, true} {
		client := &SCRAM256{Username: "alice", Password: []byte("secret")}
		mechanism, first, err := client.Start()
		if err != nil || mechanism != "SCRAM-SHA-256" {
			t.Fatal(err)
		}
		conversation := server.NewConversation()
		challenge, err := conversation.Step(string(first))
		if err != nil {
			t.Fatal(err)
		}
		proof, err := client.Next([]byte(challenge))
		if err != nil {
			t.Fatal(err)
		}
		final, err := conversation.Step(string(proof))
		if err != nil {
			t.Fatal(err)
		}
		if bad {
			final = "v=" + strings.Repeat("A", 44)
		}
		_, err = client.Next([]byte(final))
		if bad {
			if err == nil || client.Valid() {
				t.Fatal("accepted forged server proof")
			}
		} else if err != nil || !client.Valid() {
			t.Fatal("valid proof rejected", err)
		}
	}
}
func TestSCRAM256BoundsServerWork(t *testing.T) {
	for _, challenge := range []string{"r=x,s=c2FsdA==,i=1000001", "r=x,s=c2FsdA==,i=4095", "r=x,s=c2FsdA==,i=4096,i=4096", strings.Repeat("a", 16385)} {
		client := &SCRAM256{Username: "alice", Password: []byte("secret")}
		client.Start()
		if _, err := client.Next([]byte(challenge)); err == nil {
			t.Fatal("unbounded/malformed challenge accepted")
		}
	}
}
