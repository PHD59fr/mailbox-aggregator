package filestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func credentialsFixture(t *testing.T) (*Store, uuid.UUID, map[string]ImportFile) {
	t.Helper()
	s, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	original := map[string][]byte{}
	for _, name := range credentialNames {
		original[name] = importEnvelope("before")
	}
	original["auth.json"] = []byte(`{"version":1,"protocol":"OPAQUE-RFC9807","record":"before"}`)
	original["vault.json"] = []byte(`{"version":1,"kdf":"argon2id","kdf_salt":"AAAAAAAAAAAAAAAAAAAAAA","kdf_parameters":{"memory_kib":65536,"iterations":3,"parallelism":1,"key_length":32},"wrap_algorithm":"AES-256-GCM","wrapped_vmk":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","wrap_nonce":"AAAAAAAAAAAAAAAA","key_version":1}`)
	original["proxy.json"] = []byte(`{"version":1}`)
	if err := s.CreateUser(id, original); err != nil {
		t.Fatal(err)
	}
	files := map[string]ImportFile{}
	for name, data := range original {
		files[name] = ImportFile{Data: data, Revision: revision(data)}
	}
	files["auth.json"] = ImportFile{Data: []byte(`{"version":1,"protocol":"OPAQUE-RFC9807","record":"after"}`), Revision: revision(original["auth.json"])}
	for _, name := range []string{"config.enc", "accounts.enc", "identities.enc", "smtp.enc", "preferences.enc"} {
		files[name] = ImportFile{Data: importEnvelope("after"), Revision: revision(original[name])}
	}
	return s, id, files
}

func TestCredentialTransactionRecoversEveryInterruptionPoint(t *testing.T) {
	for count := 0; count <= len(credentialNames); count++ {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, id, files := credentialsFixture(t)
			journal, _ := json.Marshal(files)
			if err := atomicWrite(s.userDir(id), credentialsJournal, journal, 0600); err != nil {
				t.Fatal(err)
			}
			for _, name := range credentialNames[:count] {
				if err := atomicWrite(s.userDir(id), name, files[name].Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			restored, err := New(s.Root(), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			for name, wanted := range files {
				data, _, err := restored.ReadUserFile(id, name)
				if err != nil || !bytes.Equal(data, wanted.Data) {
					t.Fatalf("mixed generation after crash: %s: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(s.userDir(id), credentialsJournal)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("journal survived recovery")
			}
		})
	}
}

func TestCredentialConflictCannotPartiallyCommit(t *testing.T) {
	s, id, files := credentialsFixture(t)
	oldAuth, _, _ := s.ReadUserFile(id, "auth.json")
	bad := files["preferences.enc"]
	bad.Revision = "stale"
	files["preferences.enc"] = bad
	if err := s.UpdateCredentials(id, files); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	auth, _, _ := s.ReadUserFile(id, "auth.json")
	if !bytes.Equal(auth, oldAuth) {
		t.Fatal("partial auth update")
	}
	if err := os.WriteFile(filepath.Join(s.userDir(id), credentialsJournal), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(s.Root(), 1<<20); err == nil {
		t.Fatal("corrupt credential journal allowed startup")
	}
}
