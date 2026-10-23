package filestore

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDeleting UserStatus = "deleting"
)

type IndexEntry struct {
	UserID    uuid.UUID  `json:"user_id"`
	Status    UserStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
}

type indexDocument struct {
	Version int                   `json:"version"`
	Users   map[string]IndexEntry `json:"users"`
}

type UserIndex struct {
	dir string
	key []byte
	mu  sync.RWMutex
	doc indexDocument
}

func OpenUserIndex(indexDir string) (*UserIndex, error) {
	idx := &UserIndex{dir: indexDir, doc: indexDocument{Version: 1, Users: make(map[string]IndexEntry)}}
	path := filepath.Join(indexDir, "users.json")
	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return nil, err
	}
	if !missing {
		if err = json.Unmarshal(data, &idx.doc); err != nil {
			return nil, fmt.Errorf("decode user index: %w", err)
		}
	}
	if idx.doc.Version != 1 || idx.doc.Users == nil {
		return nil, errors.New("unsupported or corrupt user index")
	}
	for digest, entry := range idx.doc.Users {
		if len(digest) != 64 || entry.UserID == uuid.Nil || (entry.Status != UserActive && entry.Status != UserDeleting) {
			return nil, errors.New("invalid user index entry")
		}
	}
	users, err := directoryHasEntries(filepath.Join(filepath.Dir(indexDir), "users"))
	if err != nil {
		return nil, fmt.Errorf("inspect existing user data: %w", err)
	}
	if missing && users {
		return nil, errors.New("users.json is missing but user data already exists; restore the original user index")
	}
	deletions, err := directoryHasEntries(filepath.Join(indexDir, "deletions"))
	if err != nil {
		return nil, fmt.Errorf("inspect pending account deletions: %w", err)
	}
	idx.key, err = loadOrCreateIndexKey(indexDir, len(idx.doc.Users) == 0 && !users && !deletions)
	if err != nil {
		return nil, err
	}
	if missing {
		if err = idx.persistLocked(); err != nil {
			return nil, err
		}
	}
	return idx, nil
}

func directoryHasEntries(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return len(entries) > 0, err
}

func NormalizeLogin(login string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if len(login) < 3 || len(login) > 254 || strings.ContainsAny(login, "\x00\r\n") {
		return "", errors.New("invalid login identifier")
	}
	return login, nil
}

func (i *UserIndex) Add(login string, id uuid.UUID) error {
	normalized, err := NormalizeLogin(login)
	if err != nil {
		return err
	}
	if id == uuid.Nil {
		return ErrInvalidUserID
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	digest := i.digest(normalized)
	if _, exists := i.doc.Users[digest]; exists {
		return ErrConflict
	}
	i.doc.Users[digest] = IndexEntry{UserID: id, Status: UserActive, CreatedAt: time.Now().UTC()}
	if err = i.persistLocked(); err != nil {
		delete(i.doc.Users, digest)
		return err
	}
	return nil
}

func (i *UserIndex) Lookup(login string) (IndexEntry, bool) {
	normalized, err := NormalizeLogin(login)
	if err != nil {
		return IndexEntry{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	entry, ok := i.doc.Users[i.digest(normalized)]
	return entry, ok
}

func (i *UserIndex) MarkDeleting(id uuid.UUID) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for digest, entry := range i.doc.Users {
		if entry.UserID == id {
			entry.Status = UserDeleting
			i.doc.Users[digest] = entry
			return i.persistLocked()
		}
	}
	return os.ErrNotExist
}

func (i *UserIndex) Remove(id uuid.UUID) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	found := false
	for digest, entry := range i.doc.Users {
		if entry.UserID == id {
			delete(i.doc.Users, digest)
			found = true
		}
	}
	if !found {
		return nil
	}
	return i.persistLocked()
}

func (i *UserIndex) IsActive(id uuid.UUID) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, entry := range i.doc.Users {
		if entry.UserID == id {
			return entry.Status == UserActive
		}
	}
	return false
}

// HasUsers includes pending deletions, whose authentication state must not be
// discarded by an automatic server-key replacement.
func (i *UserIndex) HasUsers() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.doc.Users) > 0
}

func (i *UserIndex) DeletingIDs() []uuid.UUID {
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make([]uuid.UUID, 0)
	for _, entry := range i.doc.Users {
		if entry.Status == UserDeleting {
			result = append(result, entry.UserID)
		}
	}
	return result
}

func (i *UserIndex) digest(login string) string {
	mac := hmac.New(sha256.New, i.key)
	_, _ = mac.Write([]byte("mail-aggregator/login-index/v1\x00" + login))
	return fmt.Sprintf("%x", mac.Sum(nil))
}
func (i *UserIndex) persistLocked() error {
	data, err := json.MarshalIndent(i.doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(i.dir, "users.json", data, 0600)
}

func loadOrCreateIndexKey(dir string, allowCreate bool) ([]byte, error) {
	path := filepath.Join(dir, "index.key")
	if data, err := os.ReadFile(path); err == nil {
		decoded, decodeErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(decoded) != 32 {
			return nil, errors.New("invalid index key")
		}
		return decoded, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if !allowCreate {
		return nil, errors.New("index.key is missing but account data already exists; restore the original index.key instead of generating a replacement")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	defer clear(key)
	if err := installIndexKey(dir, path, key); err != nil {
		return nil, err
	}
	// Read the installed winner; simultaneous initializations must share a key.
	return loadOrCreateIndexKey(dir, false)
}

// Link a complete, synced key into place without overwriting a concurrent key.
func installIndexKey(dir, path string, key []byte) error {
	file, err := os.CreateTemp(dir, ".index-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.WriteString(base64.RawStdEncoding.EncodeToString(key) + "\n"); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Link(file.Name(), path); errors.Is(err, os.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	return syncDirectory(dir)
}
