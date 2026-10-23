// Package diagnostic inspects a stopped-instance backup without changing it.
package diagnostic

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytemare/opaque"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/vault"
	"github.com/google/uuid"
)

type Report struct {
	Users            int  `json:"users"`
	PendingDeletions int  `json:"pending_deletions"`
	Files            int  `json:"files"`
	ExternalSetup    bool `json:"external_opaque_setup"`
}

func read(root, name string, max int64) ([]byte, error) {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("missing backup component %s", name)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > max {
		return nil, fmt.Errorf("unsafe backup component %s (regular file, private permissions and bounded size required)", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return nil, errors.New("backup component grew beyond size limit")
	}
	return data, err
}
func directory(root, name string) error {
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("unsafe backup directory %s", name)
	}
	return nil
}
func Check(root, externalSetup string, maxBytes int64) (Report, error) {
	var report Report
	if maxBytes < 1024 {
		return report, errors.New("invalid diagnostic file-size limit")
	}
	for _, name := range []string{".", "index", "index/deletions", "users"} {
		if err := directory(root, name); err != nil {
			return report, err
		}
	}
	setup := strings.TrimSpace(externalSetup)
	if setup == "" {
		raw, err := read(root, "opaque_server_setup", 1<<20)
		if err != nil {
			return report, err
		}
		setup = strings.TrimSpace(string(raw))
	} else {
		report.ExternalSetup = true
	}
	conf := opaque.DefaultConfiguration()
	material, err := conf.DecodeServerKeyMaterialHex(setup)
	if err != nil {
		return report, errors.New("backup contains an invalid OPAQUE setup")
	}
	defer material.Flush()
	server, err := conf.Server()
	if err != nil {
		return report, err
	}
	if err := server.SetKeyMaterial(material); err != nil {
		return report, errors.New("invalid OPAQUE server key material")
	}
	if report.ExternalSetup {
		if _, err := os.Lstat(filepath.Join(root, "opaque_server_setup")); err == nil {
			persisted, err := read(root, "opaque_server_setup", 16<<10)
			if err != nil {
				return report, err
			}
			other, err := conf.DecodeServerKeyMaterialHex(strings.TrimSpace(string(persisted)))
			if err != nil {
				return report, errors.New("invalid persisted OPAQUE setup")
			}
			equal := other.Hex() == material.Hex()
			other.Flush()
			if !equal {
				return report, errors.New("external and persisted OPAQUE setups do not match")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, err
		}
	}
	raw, err := read(root, "index/index.key", 1024)
	if err != nil {
		return report, err
	}
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return report, errors.New("backup contains an invalid index.key")
	}
	clear(key)
	raw, err = read(root, "index/users.json", 16<<20)
	if err != nil {
		return report, err
	}
	var index struct {
		Version int                             `json:"version"`
		Users   map[string]filestore.IndexEntry `json:"users"`
	}
	if json.Unmarshal(raw, &index) != nil || index.Version != 1 || index.Users == nil {
		return report, errors.New("backup contains an invalid user index")
	}
	known := map[string]bool{}
	for digest, entry := range index.Users {
		hash, err := hex.DecodeString(digest)
		if err != nil || len(hash) != 32 || entry.UserID == uuid.Nil || (entry.Status != filestore.UserActive && entry.Status != filestore.UserDeleting) {
			return report, errors.New("backup contains an invalid user entry")
		}
		id := entry.UserID.String()
		if known[id] {
			return report, errors.New("duplicate indexed user")
		}
		known[id] = true
		if entry.Status == filestore.UserDeleting {
			report.PendingDeletions++
			continue
		}
		if err := directory(root, "users/"+id); err != nil {
			return report, err
		}
		for _, journal := range []string{".vault-import.json", ".credentials-update.json"} {
			if _, err := os.Lstat(filepath.Join(root, "users", id, journal)); err == nil {
				return report, errors.New("backup has a pending encrypted transaction; recover in an isolated copy before validation")
			}
		}
		for _, name := range []string{"auth.json", "vault.json", "config.enc", "accounts.enc", "identities.enc", "smtp.enc", "preferences.enc"} {
			data, err := read(root, "users/"+id+"/"+name, maxBytes)
			if err != nil {
				return report, err
			}
			switch name {
			case "auth.json":
				var record struct {
					Version          int `json:"version"`
					Protocol, Record string
				}
				if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.Protocol != "OPAQUE-RFC9807" {
					return report, errors.New("invalid backed-up authentication record")
				}
				wire, err := base64.RawURLEncoding.DecodeString(record.Record)
				if err != nil {
					return report, errors.New("invalid backed-up registration record")
				}
				if _, err := server.Deserialize.RegistrationRecord(wire); err != nil {
					return report, errors.New("invalid backed-up OPAQUE registration record")
				}
			case "vault.json":
				if err := vault.ValidateMetadataJSON(data); err != nil {
					return report, errors.New("invalid backed-up vault metadata")
				}
			default:
				if err := vault.ValidateEncryptedFileJSON(data); err != nil {
					return report, errors.New("invalid backed-up encrypted collection")
				}
			}
			report.Files++
		}
		if _, err := os.Lstat(filepath.Join(root, "users", id, "proxy.json")); err == nil {
			data, err := read(root, "users/"+id+"/proxy.json", maxBytes)
			if err != nil {
				return report, err
			}
			var proxy struct {
				Version                 int    `json:"version"`
				IMAP                    string `json:"imap_account_id"`
				SMTP                    string `json:"smtp_config_id"`
				Salt, Nonce, Ciphertext []byte
			}
			if json.Unmarshal(data, &proxy) != nil || proxy.Version != 1 {
				return report, errors.New("invalid backed-up gateway record")
			}
			if proxy.IMAP != "" || proxy.SMTP != "" || len(proxy.Ciphertext) > 0 {
				if len(proxy.Salt) != 16 || len(proxy.Nonce) != 12 || len(proxy.Ciphertext) < 16 {
					return report, errors.New("invalid backed-up gateway encryption")
				}
			}
			report.Files++
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, err
		}
		report.Users++
	}
	entries, err := os.ReadDir(filepath.Join(root, "users"))
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if _, err := filestore.ParseUserID(entry.Name()); err != nil || !known[entry.Name()] || !entry.IsDir() {
			return report, errors.New("backup contains an orphaned or unrecognized user directory")
		}
	}
	deletions, err := os.ReadDir(filepath.Join(root, "index/deletions"))
	if err != nil {
		return report, err
	}
	for _, entry := range deletions {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return report, errors.New("invalid deletion journal in backup")
		}
		id, err := filestore.ParseUserID(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return report, errors.New("invalid deletion journal identifier")
		}
		raw, err := read(root, "index/deletions/"+entry.Name(), 16<<10)
		if err != nil {
			return report, err
		}
		var journal struct {
			Version int       `json:"version"`
			UserID  uuid.UUID `json:"user_id"`
			Stage   string    `json:"stage"`
		}
		if json.Unmarshal(raw, &journal) != nil || journal.Version != 1 || journal.UserID != id || journal.Stage == "" {
			return report, errors.New("invalid deletion journal contents")
		}
	}
	return report, nil
}
