package filestore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestMissingIndexKeyRequiresRestorationForExistingAccounts(t *testing.T) {
	for _, status := range []UserStatus{UserActive, UserDeleting} {
		t.Run(string(status), func(t *testing.T) {
			store, _ := New(t.TempDir(), 1<<20)
			index, err := OpenUserIndex(store.IndexDirectory())
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.New()
			if err = index.Add("existing@example.test", id); err != nil {
				t.Fatal(err)
			}
			if status == UserDeleting {
				if err = index.MarkDeleting(id); err != nil {
					t.Fatal(err)
				}
			}
			keyPath := filepath.Join(store.IndexDirectory(), "index.key")
			key, _ := os.ReadFile(keyPath)
			indexPath := filepath.Join(store.IndexDirectory(), "users.json")
			before, _ := os.ReadFile(indexPath)
			if err = os.Remove(keyPath); err != nil {
				t.Fatal(err)
			}
			if _, err = OpenUserIndex(store.IndexDirectory()); err == nil || !strings.Contains(err.Error(), "restore the original index.key") {
				t.Fatalf("missing original key did not stop startup: %v", err)
			}
			if _, err = os.Stat(keyPath); !os.IsNotExist(err) {
				t.Fatal("a replacement key was generated")
			}
			after, _ := os.ReadFile(indexPath)
			if !bytes.Equal(before, after) {
				t.Fatal("existing index changed")
			}
			if err = os.WriteFile(keyPath, key, 0600); err != nil {
				t.Fatal(err)
			}
			restored, err := OpenUserIndex(store.IndexDirectory())
			if err != nil {
				t.Fatal(err)
			}
			entry, found := restored.Lookup("existing@example.test")
			if !found || entry.UserID != id || entry.Status != status {
				t.Fatal("restoring the original key did not restore the login")
			}
		})
	}
}

func TestIndexDoesNotGenerateFilesOverOrphanedUserData(t *testing.T) {
	for _, missing := range []string{"key", "index", "both"} {
		t.Run(missing, func(t *testing.T) {
			store, _ := New(t.TempDir(), 1<<20)
			if _, err := OpenUserIndex(store.IndexDirectory()); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateUser(uuid.New(), map[string][]byte{"auth.json": []byte("{}")}); err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{"key": filepath.Join(store.IndexDirectory(), "index.key"), "index": filepath.Join(store.IndexDirectory(), "users.json")}
			for name, path := range paths {
				if missing == name || missing == "both" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := OpenUserIndex(store.IndexDirectory()); err == nil {
				t.Fatal("incomplete account data was accepted")
			}
			for name, path := range paths {
				if missing == name || missing == "both" {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("missing %s was silently generated", name)
					}
				}
			}
		})
	}
}

func TestIndexKeyIsNotGeneratedOverInvalidIndexOrPendingDeletion(t *testing.T) {
	for _, state := range []string{"invalid index", "pending deletion"} {
		t.Run(state, func(t *testing.T) {
			store, _ := New(t.TempDir(), 1<<20)
			path := filepath.Join(store.IndexDirectory(), "users.json")
			if state == "pending deletion" {
				path = filepath.Join(store.IndexDirectory(), "deletions", uuid.NewString()+".json")
			}
			if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenUserIndex(store.IndexDirectory()); err == nil {
				t.Fatal("invalid or incomplete account state was accepted")
			}
			if _, err := os.Stat(filepath.Join(store.IndexDirectory(), "index.key")); !os.IsNotExist(err) {
				t.Fatal("replacement key created")
			}
		})
	}
}

func TestConcurrentFreshIndexInitializationKeepsOnePrivateKey(t *testing.T) {
	store, _ := New(t.TempDir(), 1<<20)
	const count = 8
	results := make(chan *UserIndex, count)
	errors := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			index, err := OpenUserIndex(store.IndexDirectory())
			if err != nil {
				errors <- err
				return
			}
			results <- index
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var first []byte
	for index := range results {
		if first == nil {
			first = index.key
		}
		if !bytes.Equal(first, index.key) {
			t.Fatal("initializations received different keys")
		}
	}
	info, err := os.Stat(filepath.Join(store.IndexDirectory(), "index.key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("index key is not private")
	}
	leftovers, _ := filepath.Glob(filepath.Join(store.IndexDirectory(), ".index-key-*"))
	if len(leftovers) != 0 {
		t.Fatal("temporary key files retained")
	}
}
