package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/google/uuid"
)

func setupStore(t *testing.T) (*filestore.Store, *filestore.UserIndex) {
	t.Helper()
	store, err := filestore.New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	return store, index
}

func TestServerSetupIsGeneratedOnceAndPersistedPrivately(t *testing.T) {
	store, index := setupStore(t)
	first, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "generated" || first.Path != filepath.Join(store.Root(), SetupFilename) {
		t.Fatal("unexpected generated setup metadata")
	}
	if _, err = NewService(first.Value, nil, nil, nil, nil); err != nil {
		t.Fatal("generated setup is unusable")
	}
	data, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(first.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("setup file is not private")
	}
	second, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(first.Path)
	if second.Source != "file" || second.Value != first.Value || !bytes.Equal(data, after) {
		t.Fatal("restart changed the authentication identity")
	}
	if err = os.Chmod(first.Path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveServerSetup(store, index, ""); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(first.Path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("loose file permissions were not restricted")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(store.Root(), ".opaque-server-setup-*")); len(leftovers) != 0 {
		t.Fatal("temporary setup files retained")
	}
}

func TestServerSetupSupportsExplicitKeysWithoutCopyingThemIntoData(t *testing.T) {
	store, index := setupStore(t)
	value, err := SetupHex()
	if err != nil {
		t.Fatal(err)
	}
	// An existing deployment with externally managed setup can still start.
	if err = index.Add("existing@example.test", uuid.New()); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveServerSetup(store, index, value)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != "environment" || resolved.Value != value {
		t.Fatal("explicit setup was ignored")
	}
	if _, err = os.Stat(filepath.Join(store.Root(), SetupFilename)); !os.IsNotExist(err) {
		t.Fatal("external setup was persisted without opt-in")
	}
}

func TestServerSetupRejectsReplacementAndInvalidMaterial(t *testing.T) {
	store, index := setupStore(t)
	first, err := ResolveServerSetup(store, index, "")
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(first.Path)
	other, err := SetupHex()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveServerSetup(store, index, other); err == nil {
		t.Fatal("conflicting external key replaced the existing identity")
	}
	if _, err = ResolveServerSetup(store, index, strings.ToUpper(first.Value)); err != nil {
		t.Fatal("equivalent hex encoding rejected")
	}
	after, _ := os.ReadFile(first.Path)
	if !bytes.Equal(original, after) {
		t.Fatal("existing key was overwritten")
	}
	for _, input := range []string{"", "private-malformed-key", strings.Repeat("x", maxSetupBytes+1)} {
		if err = os.WriteFile(first.Path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = ResolveServerSetup(store, index, ""); err == nil || strings.Contains(err.Error(), "private-malformed-key") {
			t.Fatal("invalid key was replaced or exposed in an error")
		}
		unchanged, _ := os.ReadFile(first.Path)
		if string(unchanged) != input {
			t.Fatal("invalid persisted setup was silently regenerated")
		}
	}
}

func TestServerSetupNeverGeneratesOverExistingAccountState(t *testing.T) {
	for _, state := range []string{"index", "orphan"} {
		t.Run(state, func(t *testing.T) {
			store, index := setupStore(t)
			if state == "index" {
				if err := index.Add("existing@example.test", uuid.New()); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(store.Root(), "users", uuid.NewString()), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ResolveServerSetup(store, index, ""); err == nil || !strings.Contains(err.Error(), "restore") {
				t.Fatal("lost authentication setup was silently replaced")
			}
			if _, err := os.Stat(filepath.Join(store.Root(), SetupFilename)); !os.IsNotExist(err) {
				t.Fatal("setup created for pre-existing accounts")
			}
		})
	}
}

func TestServerSetupRejectsSymlinksWithoutFollowingThem(t *testing.T) {
	store, index := setupStore(t)
	outside := filepath.Join(t.TempDir(), "original")
	if err := os.WriteFile(outside, []byte("private-marker"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Root(), SetupFilename)); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveServerSetup(store, index, ""); err == nil {
		t.Fatal("setup symlink followed")
	}
	data, _ := os.ReadFile(outside)
	info, _ := os.Stat(outside)
	if string(data) != "private-marker" || info.Mode().Perm() != 0644 {
		t.Fatal("symlink target was changed")
	}
}

func TestConcurrentSetupInitializationUsesOneCompleteKey(t *testing.T) {
	store, index := setupStore(t)
	var wg sync.WaitGroup
	values := make(chan ServerSetup, 16)
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := ResolveServerSetup(store, index, "")
			if err != nil {
				failures <- err
			} else {
				values <- value
			}
		}()
	}
	wg.Wait()
	close(values)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	winner := ""
	generated := 0
	for value := range values {
		if winner == "" {
			winner = value.Value
		}
		if value.Value != winner {
			t.Fatal("concurrent startups used different keys")
		}
		if value.Source == "generated" {
			generated++
		}
	}
	if generated != 1 {
		t.Fatal("setup was not installed exactly once")
	}
}
