package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/example/mail-aggregator/internal/filestore"
)

type SessionRevoker interface{ RevokeUser(uuid.UUID) }
type OperationCanceler interface{ CancelUser(uuid.UUID) }

type Service struct {
	store    *filestore.Store
	index    *filestore.UserIndex
	sessions SessionRevoker
	ops      OperationCanceler
	mu       sync.Mutex
	deleting map[uuid.UUID]struct{}
}

type RegistrationFiles struct {
	AuthJSON        []byte
	VaultJSON       []byte
	ConfigEncrypted []byte
	Accounts        []byte
	Identities      []byte
	SMTP            []byte
	Preferences     []byte
}

func NewService(store *filestore.Store, index *filestore.UserIndex, sessions SessionRevoker, ops OperationCanceler) *Service {
	return &Service{store: store, index: index, sessions: sessions, ops: ops, deleting: make(map[uuid.UUID]struct{})}
}

func (s *Service) Register(login string, files RegistrationFiles) (uuid.UUID, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return uuid.Nil, err
	}
	return s.RegisterWithID(login, id, files)
}

func (s *Service) RegisterWithID(login string, id uuid.UUID, files RegistrationFiles) (uuid.UUID, error) {
	if _, exists := s.index.Lookup(login); exists {
		return uuid.Nil, filestore.ErrConflict
	}
	if id == uuid.Nil {
		return uuid.Nil, filestore.ErrInvalidUserID
	}
	var err error
	all := map[string][]byte{
		"auth.json": files.AuthJSON, "vault.json": files.VaultJSON, "config.enc": files.ConfigEncrypted,
		"accounts.enc": files.Accounts, "identities.enc": files.Identities, "smtp.enc": files.SMTP,
		"preferences.enc": files.Preferences,
	}
	if err = s.store.CreateUser(id, all); err != nil {
		return uuid.Nil, err
	}
	if err = s.index.Add(login, id); err != nil {
		_ = s.store.WithExclusiveUser(id, func(dir string) error { return s.store.RemoveUserLocked(dir) })
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Service) IsActive(id uuid.UUID) bool {
	s.mu.Lock()
	_, deleting := s.deleting[id]
	s.mu.Unlock()
	return !deleting && s.index.IsActive(id)
}

func (s *Service) DeleteOwn(id uuid.UUID) error {
	if id == uuid.Nil {
		return filestore.ErrInvalidUserID
	}
	s.mu.Lock()
	if _, exists := s.deleting[id]; exists {
		s.mu.Unlock()
		return filestore.ErrConflict
	}
	s.deleting[id] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.deleting, id); s.mu.Unlock() }()
	if err := s.writeTombstone(id, "requested"); err != nil {
		return err
	}
	if err := s.index.MarkDeleting(id); err != nil {
		return err
	}
	return s.finishDeletion(id)
}

func (s *Service) RecoverDeletions() error {
	ids := make(map[uuid.UUID]struct{})
	for _, id := range s.index.DeletingIDs() {
		ids[id] = struct{}{}
	}
	dir := filepath.Join(s.store.IndexDirectory(), "deletions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id, parseErr := filestore.ParseUserID(entry.Name()[:len(entry.Name())-len(".json")])
		if parseErr != nil {
			return fmt.Errorf("invalid deletion journal name: %w", parseErr)
		}
		ids[id] = struct{}{}
	}
	for id := range ids {
		if err = s.index.MarkDeleting(id); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = s.finishDeletion(id); err != nil {
			return fmt.Errorf("resume deletion %s: %w", id, err)
		}
	}
	return nil
}

func (s *Service) finishDeletion(id uuid.UUID) error {
	if s.ops != nil {
		s.ops.CancelUser(id)
	}
	if s.sessions != nil {
		s.sessions.RevokeUser(id)
	}
	err := s.store.WithExclusiveUser(id, func(dir string) error { return s.store.RemoveUserLocked(dir) })
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = s.index.Remove(id); err != nil {
		return err
	}
	journal := filepath.Join(s.store.IndexDirectory(), "deletions", id.String()+".json")
	if err = os.Remove(journal); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	d, err := os.Open(filepath.Dir(journal))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

type tombstone struct {
	Version   int       `json:"version"`
	UserID    uuid.UUID `json:"user_id"`
	Stage     string    `json:"stage"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) writeTombstone(id uuid.UUID, stage string) error {
	data, err := json.Marshal(tombstone{Version: 1, UserID: id, Stage: stage, CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	dir := filepath.Join(s.store.IndexDirectory(), "deletions")
	temp, err := os.CreateTemp(dir, ".deletion-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		_ = os.Remove(name)
	}()
	if err = temp.Chmod(0600); err != nil {
		return err
	}
	if _, err = temp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	closed = true
	if err = os.Rename(name, filepath.Join(dir, id.String()+".json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
