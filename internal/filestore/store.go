package filestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

var (
	ErrInvalidUserID = errors.New("invalid user ID")
	ErrInvalidName   = errors.New("invalid file name")
	ErrConflict      = errors.New("file revision conflict")
	ErrTooLarge      = errors.New("file exceeds size limit")
	ErrUnsafePath    = errors.New("unsafe filesystem path")
)

var allowedUserFiles = map[string]struct{}{
	"auth.json": {}, "vault.json": {}, "config.enc": {}, "accounts.enc": {},
	"identities.enc": {}, "smtp.enc": {}, "preferences.enc": {},
	"proxy.json": {},
	"mfa.json":   {},
}

type Store struct {
	root     string
	usersDir string
	indexDir string
	maxBytes int64
	locksMu  sync.Mutex
	locks    map[uuid.UUID]*sync.RWMutex
}

func New(root string, maxBytes int64) (*Store, error) {
	if maxBytes <= 0 {
		return nil, errors.New("maxBytes must be positive")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if err = ensureDirectory(abs, 0700); err != nil {
		return nil, err
	}
	users := filepath.Join(abs, "users")
	index := filepath.Join(abs, "index")
	for _, dir := range []string{users, index, filepath.Join(index, "deletions")} {
		if err = ensureDirectory(dir, 0700); err != nil {
			return nil, err
		}
	}
	store := &Store{root: abs, usersDir: users, indexDir: index, maxBytes: maxBytes, locks: make(map[uuid.UUID]*sync.RWMutex)}
	if err := store.recoverImports(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Root() string           { return s.root }
func (s *Store) IndexDirectory() string { return s.indexDir }

func ParseUserID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id.String() != raw || id.Variant() != uuid.RFC4122 {
		return uuid.Nil, ErrInvalidUserID
	}
	return id, nil
}

func (s *Store) CreateUser(id uuid.UUID, files map[string][]byte) error {
	if id == uuid.Nil {
		return ErrInvalidUserID
	}
	lock := s.userLock(id)
	lock.Lock()
	defer lock.Unlock()
	dir := s.userDir(id)
	if err := rejectSymlink(s.usersDir); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("create user directory: %w", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	for name, content := range files {
		if err := validateName(name); err != nil {
			return err
		}
		if int64(len(content)) > s.maxBytes {
			return ErrTooLarge
		}
		if err := atomicWrite(dir, name, content, 0600); err != nil {
			return err
		}
	}
	if err := syncDirectory(s.usersDir); err != nil {
		return err
	}
	created = false
	return nil
}

func (s *Store) ReadUserFile(id uuid.UUID, name string) ([]byte, string, error) {
	if id == uuid.Nil {
		return nil, "", ErrInvalidUserID
	}
	if err := validateName(name); err != nil {
		return nil, "", err
	}
	lock := s.userLock(id)
	lock.Lock()
	defer lock.Unlock()
	dir := s.userDir(id)
	if err := validateUserDirectory(dir); err != nil {
		return nil, "", err
	}
	if err := s.finishTransactions(dir); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrUnsafePath
	}
	if info.Size() > s.maxBytes {
		return nil, "", ErrTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, s.maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > s.maxBytes {
		return nil, "", ErrTooLarge
	}
	return data, revision(data), nil
}

func (s *Store) WriteUserFile(id uuid.UUID, name string, data []byte, expectedRevision string, expectedAuth ...string) (string, error) {
	if id == uuid.Nil {
		return "", ErrInvalidUserID
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	if int64(len(data)) > s.maxBytes {
		return "", ErrTooLarge
	}
	lock := s.userLock(id)
	lock.Lock()
	defer lock.Unlock()
	dir := s.userDir(id)
	if err := validateUserDirectory(dir); err != nil {
		return "", err
	}
	if err := s.finishTransactions(dir); err != nil {
		return "", err
	}
	if err := s.checkAuthRevision(dir, expectedAuth); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnsafePath
		}
		if info.Size() > s.maxBytes {
			return "", ErrTooLarge
		}
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		if expectedRevision == "" || revision(current) != expectedRevision {
			return "", ErrConflict
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else if expectedRevision != "*" {
		return "", ErrConflict
	}
	if err := atomicWrite(dir, name, data, 0600); err != nil {
		return "", err
	}
	return revision(data), nil
}

func (s *Store) WithExclusiveUser(id uuid.UUID, fn func(userDir string) error) error {
	if id == uuid.Nil {
		return ErrInvalidUserID
	}
	lock := s.userLock(id)
	lock.Lock()
	defer lock.Unlock()
	dir := s.userDir(id)
	if err := validateUserDirectory(dir); err != nil {
		return err
	}
	return fn(dir)
}

func (s *Store) RemoveUserLocked(userDir string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(userDir))
	if err != nil {
		return err
	}
	if parent != s.usersDir || userDir == s.usersDir || filepath.Dir(userDir) != s.usersDir {
		return ErrUnsafePath
	}
	base := filepath.Base(userDir)
	if _, err = ParseUserID(base); err != nil {
		return ErrUnsafePath
	}
	if err = validateUserDirectory(userDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.RemoveAll(userDir); err != nil {
		return err
	}
	return syncDirectory(s.usersDir)
}

func (s *Store) userDir(id uuid.UUID) string { return filepath.Join(s.usersDir, id.String()) }
func (s *Store) userLock(id uuid.UUID) *sync.RWMutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if lock := s.locks[id]; lock != nil {
		return lock
	}
	lock := &sync.RWMutex{}
	s.locks[id] = lock
	return lock
}
func validateName(name string) error {
	if _, ok := allowedUserFiles[name]; !ok || filepath.Base(name) != name {
		return ErrInvalidName
	}
	return nil
}
func revision(data []byte) string {
	sum := sha256.Sum256(data)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`
}

func ensureDirectory(path string, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		return os.Chmod(path, mode)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafePath
	}
	return nil
}
func validateUserDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	return nil
}
