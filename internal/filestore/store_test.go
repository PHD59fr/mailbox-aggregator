package filestore

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestParseUserIDStrict(t *testing.T) {
	id := uuid.New()
	if parsed, err := ParseUserID(id.String()); err != nil || parsed != id {
		t.Fatalf("valid UUID rejected: %v", err)
	}
	for _, value := range []string{"../data", id.String() + "/x", "", "NOT-A-UUID", id.String()[0:35]} {
		if _, err := ParseUserID(value); !errors.Is(err, ErrInvalidUserID) {
			t.Errorf("%q: got %v", value, err)
		}
	}
}

func TestStoreRejectsTraversalAndSymlink(t *testing.T) {
	store, err := New(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err = store.CreateUser(id, map[string][]byte{"accounts.enc": []byte("one")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.ReadUserFile(id, "../accounts.enc"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("traversal: %v", err)
	}
	userDir := filepath.Join(store.usersDir, id.String())
	if err = os.Remove(filepath.Join(userDir, "accounts.enc")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(userDir, "accounts.enc")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.ReadUserFile(id, "accounts.enc"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink read: %v", err)
	}
	if _, err = store.WriteUserFile(id, "accounts.enc", []byte("bad"), "*"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink write: %v", err)
	}
}

func TestOptimisticConcurrentWriteAllowsOneWinner(t *testing.T) {
	store, err := New(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err = store.CreateUser(id, map[string][]byte{"config.enc": []byte("initial")}); err != nil {
		t.Fatal(err)
	}
	_, rev, err := store.ReadUserFile(id, "config.enc")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			_, writeErr := store.WriteUserFile(id, "config.enc", []byte(value), rev)
			results <- writeErr
		}(value)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		if result == nil {
			successes++
		} else if errors.Is(result, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestCreateUserPermissionsAndLimits(t *testing.T) {
	store, err := New(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err = store.CreateUser(id, map[string][]byte{"auth.json": []byte("12345678")}); err != nil {
		t.Fatal(err)
	}
	dirInfo, _ := os.Stat(filepath.Join(store.usersDir, id.String()))
	fileInfo, _ := os.Stat(filepath.Join(store.usersDir, id.String(), "auth.json"))
	if dirInfo.Mode().Perm() != 0700 || fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("permissions are %o/%o", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
	if _, err = store.WriteUserFile(id, "auth.json", []byte("123456789"), "invalid"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size limit: %v", err)
	}
}
