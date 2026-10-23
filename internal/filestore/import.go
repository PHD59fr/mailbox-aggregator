package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/example/mail-aggregator/internal/vault"
	"github.com/google/uuid"
)

const importJournal = ".vault-import.json"

var importNames = []string{"accounts.enc", "smtp.enc", "preferences.enc"}

type ImportFile struct {
	Data     json.RawMessage `json:"data"`
	Revision string          `json:"revision"`
}

// WriteUserFiles validates every revision before committing. The durable
// journal contains encrypted envelopes only. Once installed it is rolled
// forward before any subsequent collection read/write, including on restart.
// A lost response is ambiguous: clients must reload, never automatically retry.
func (s *Store) WriteUserFiles(id uuid.UUID, files map[string]ImportFile, expectedAuth ...string) error {
	if id == uuid.Nil {
		return ErrInvalidUserID
	}
	if err := s.validateImport(files); err != nil {
		return err
	}
	lock := s.userLock(id)
	lock.Lock()
	defer lock.Unlock()
	dir := s.userDir(id)
	if err := validateUserDirectory(dir); err != nil {
		return err
	}
	if err := s.finishTransactions(dir); err != nil {
		return err
	}
	if err := s.checkAuthRevision(dir, expectedAuth); err != nil {
		return err
	}
	for _, name := range importNames {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrUnsafePath
		}
		if info.Size() > s.maxBytes {
			return ErrTooLarge
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if files[name].Revision == "" || files[name].Revision != revision(current) {
			return ErrConflict
		}
	}
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	if err := atomicWrite(dir, importJournal, data, 0600); err != nil {
		return err
	}
	return s.finishTransactions(dir)
}

func (s *Store) validateImport(files map[string]ImportFile) error {
	if len(files) != len(importNames) {
		return ErrInvalidName
	}
	for _, name := range importNames {
		file, ok := files[name]
		if !ok {
			return ErrInvalidName
		}
		if int64(len(file.Data)) > s.maxBytes {
			return ErrTooLarge
		}
		if err := vault.ValidateEncryptedFileJSON(file.Data); err != nil {
			return fmt.Errorf("invalid import envelope: %w", err)
		}
	}
	return nil
}

func (s *Store) finishImport(dir string) error {
	path := filepath.Join(dir, importJournal)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafePath
	}
	if info.Size() > 4*s.maxBytes+4096 {
		return ErrTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var files map[string]ImportFile
	if err := json.Unmarshal(data, &files); err != nil {
		return fmt.Errorf("recover vault import: %w", err)
	}
	if err := s.validateImport(files); err != nil {
		return err
	}
	for _, name := range importNames {
		if err := atomicWrite(dir, name, files[name].Data, 0600); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func (s *Store) recoverImports() error {
	entries, err := os.ReadDir(s.usersDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := ParseUserID(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join(s.usersDir, entry.Name())
		if err := validateUserDirectory(dir); err != nil {
			return err
		}
		if err := s.finishTransactions(dir); err != nil {
			return fmt.Errorf("recover encrypted configuration import: %w", err)
		}
	}
	return nil
}
