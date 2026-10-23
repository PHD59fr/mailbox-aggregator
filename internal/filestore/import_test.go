package filestore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func importEnvelope(value string) []byte {
	data, _ := json.Marshal(map[string]any{"version": 1, "algorithm": "AES-256-GCM", "key_version": 1, "nonce": "AAAAAAAAAAAAAAAA", "ciphertext": base64.RawStdEncoding.EncodeToString([]byte(value + "01234567890123456789"))})
	return data
}
func importFixture(t *testing.T) (*Store, uuid.UUID, map[string]ImportFile) {
	t.Helper()
	store, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	original := map[string][]byte{}
	files := map[string]ImportFile{}
	for _, name := range importNames {
		original[name] = importEnvelope("old")
		files[name] = ImportFile{Data: importEnvelope("new"), Revision: revision(original[name])}
	}
	if err := store.CreateUser(id, original); err != nil {
		t.Fatal(err)
	}
	return store, id, files
}
func TestImportAllRevisionsAreCheckedBeforeWriting(t *testing.T) {
	store, id, files := importFixture(t)
	wrong := files["smtp.enc"]
	wrong.Revision = "stale"
	files["smtp.enc"] = wrong
	if err := store.WriteUserFiles(id, files); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale import: %v", err)
	}
	for _, name := range importNames {
		data, _, err := store.ReadUserFile(id, name)
		if err != nil || !bytes.Equal(data, importEnvelope("old")) {
			t.Fatal("conflicting import changed a collection")
		}
	}
}
func TestImportCompletesAndRemovesJournal(t *testing.T) {
	store, id, files := importFixture(t)
	if err := store.WriteUserFiles(id, files); err != nil {
		t.Fatal(err)
	}
	for _, name := range importNames {
		data, _, err := store.ReadUserFile(id, name)
		if err != nil || !bytes.Equal(data, files[name].Data) {
			t.Fatal("incomplete import")
		}
	}
	if _, err := os.Stat(filepath.Join(store.userDir(id), importJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal left behind")
	}
}
func TestCommittedImportRecoversAfterInterruption(t *testing.T) {
	store, id, files := importFixture(t)
	data, _ := json.Marshal(files)
	if err := atomicWrite(store.userDir(id), importJournal, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(store.userDir(id), "accounts.enc", files["accounts.enc"].Data, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := New(store.Root(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range importNames {
		actual, _, err := restored.ReadUserFile(id, name)
		if err != nil || !bytes.Equal(actual, files[name].Data) {
			t.Fatal("restart exposed mixed collections")
		}
	}
}
func TestInvalidImportAndCorruptRecoveryFailClosed(t *testing.T) {
	store, id, files := importFixture(t)
	bad := files["smtp.enc"]
	bad.Data = []byte(`{"password":"must-never-be-persisted"}`)
	files["smtp.enc"] = bad
	if err := store.WriteUserFiles(id, files); err == nil {
		t.Fatal("plaintext import accepted")
	}
	if err := os.WriteFile(filepath.Join(store.userDir(id), importJournal), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadUserFile(id, "accounts.enc"); err == nil {
		t.Fatal("corrupt committed import ignored")
	}
	if _, err := New(store.Root(), 1<<20); err == nil {
		t.Fatal("corrupt recovery allowed startup")
	}
}
