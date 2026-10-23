package filestore

import (
	"encoding/json"
	"errors"
	"github.com/example/mail-aggregator/internal/vault"
	"github.com/google/uuid"
	"os"
	"path/filepath"
)

const credentialsJournal = ".credentials-update.json"

var credentialNames = []string{"auth.json", "vault.json", "config.enc", "accounts.enc", "identities.enc", "smtp.enc", "preferences.enc", "proxy.json"}

func (s *Store) checkAuthRevision(dir string, expected []string) error {
	if len(expected) == 0 || expected[0] == "" {
		return nil
	}
	path := filepath.Join(dir, "auth.json")
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > s.maxBytes {
		return ErrUnsafePath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if revision(data) != expected[0] {
		return ErrConflict
	}
	return nil
}
func (s *Store) validateCredentials(files map[string]ImportFile) error {
	if len(files) != len(credentialNames) {
		return ErrInvalidName
	}
	for _, name := range credentialNames {
		file, ok := files[name]
		if !ok {
			return ErrInvalidName
		}
		if int64(len(file.Data)) > s.maxBytes {
			return ErrTooLarge
		}
		switch name {
		case "vault.json":
			if err := vault.ValidateMetadataJSON(file.Data); err != nil {
				return err
			}
		case "auth.json":
			var rec struct {
				Version          int `json:"version"`
				Protocol, Record string
			}
			if json.Unmarshal(file.Data, &rec) != nil || rec.Version != 1 || rec.Protocol != "OPAQUE-RFC9807" || rec.Record == "" {
				return errors.New("invalid authentication record")
			}
		case "proxy.json":
			if string(file.Data) != `{"version":1}` {
				return errors.New("credential rotation must disable gateway credentials")
			}
		default:
			if err := vault.ValidateEncryptedFileJSON(file.Data); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateCredentials commits auth metadata and all encrypted collections together.
// Auth revisions prevent a request from an old session writing ciphertext after
// a key rotation, even if it passed HTTP middleware before the commit.
func (s *Store) UpdateCredentials(id uuid.UUID, files map[string]ImportFile) error {
	if id == uuid.Nil {
		return ErrInvalidUserID
	}
	if err := s.validateCredentials(files); err != nil {
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
	for _, name := range credentialNames {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && name == "proxy.json" && files[name].Revision == "*" {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > s.maxBytes {
			return ErrUnsafePath
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if revision(current) != files[name].Revision {
			return ErrConflict
		}
	}
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	if err := atomicWrite(dir, credentialsJournal, data, 0600); err != nil {
		return err
	}
	return s.finishCredentials(dir)
}
func (s *Store) finishCredentials(dir string) error {
	path := filepath.Join(dir, credentialsJournal)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 10*s.maxBytes+4096 {
		return ErrUnsafePath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var files map[string]ImportFile
	if err := json.Unmarshal(raw, &files); err != nil {
		return err
	}
	if err := s.validateCredentials(files); err != nil {
		return err
	}
	// Readers hold the same lock, and startup completes this journal before auth.
	for _, name := range credentialNames {
		if err := atomicWrite(dir, name, files[name].Data, 0600); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(dir)
}
func (s *Store) finishTransactions(dir string) error {
	_, credentialErr := os.Lstat(filepath.Join(dir, credentialsJournal))
	_, importErr := os.Lstat(filepath.Join(dir, importJournal))
	if credentialErr == nil && importErr == nil {
		return errors.New("conflicting pending vault transactions")
	}
	if err := s.finishCredentials(dir); err != nil {
		return err
	}
	return s.finishImport(dir)
}
