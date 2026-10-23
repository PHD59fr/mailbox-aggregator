package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytemare/opaque"
	"github.com/example/mail-aggregator/internal/filestore"
)

const SetupFilename = "opaque_server_setup"
const maxSetupBytes = 16 << 10

type ServerSetup struct {
	Value  string
	Source string
	Path   string
}

// ResolveServerSetup preserves explicit operator configuration. Without it, a
// new installation gets one private, durable key in its persistent data volume.
// Existing accounts with a lost key require restoration, never replacement.
func ResolveServerSetup(store *filestore.Store, index *filestore.UserIndex, explicit string) (ServerSetup, error) {
	path := filepath.Join(store.Root(), SetupFilename)
	var configured string
	var err error
	if strings.TrimSpace(explicit) != "" {
		configured, err = validateSetup(explicit)
		if err != nil {
			return ServerSetup{}, errors.New("invalid OPAQUE_SERVER_SETUP; restore a valid existing setup")
		}
	}
	persisted, err := readSetup(path)
	if err == nil {
		if configured != "" && subtle.ConstantTimeCompare([]byte(configured), []byte(persisted)) != 1 {
			return ServerSetup{}, errors.New("OPAQUE_SERVER_SETUP differs from opaque_server_setup; restore the matching key instead of replacing the authentication identity")
		}
		return ServerSetup{Value: persisted, Source: "file", Path: path}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ServerSetup{}, fmt.Errorf("load %s: %w", SetupFilename, err)
	}
	if configured != "" {
		return ServerSetup{Value: configured, Source: "environment"}, nil
	}
	users, err := os.ReadDir(filepath.Join(store.Root(), "users"))
	if err != nil {
		return ServerSetup{}, fmt.Errorf("inspect existing accounts: %w", err)
	}
	if index.HasUsers() || len(users) > 0 {
		return ServerSetup{}, errors.New("opaque_server_setup is missing but account data already exists; restore the original file or provide the original OPAQUE_SERVER_SETUP")
	}
	generated, err := SetupHex()
	if err != nil {
		return ServerSetup{}, errors.New("could not generate OPAQUE server setup")
	}
	created, err := installSetup(store.Root(), path, generated)
	if err != nil {
		return ServerSetup{}, fmt.Errorf("persist %s: %w", SetupFilename, err)
	}
	// Read the installed winner back: another startup may have created it first.
	persisted, err = readSetup(path)
	if err != nil {
		return ServerSetup{}, fmt.Errorf("load persisted %s: %w", SetupFilename, err)
	}
	source := "file"
	if created {
		source = "generated"
	}
	return ServerSetup{Value: persisted, Source: source, Path: path}, nil
}

func validateSetup(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > maxSetupBytes {
		return "", errors.New("invalid OPAQUE setup")
	}
	conf := opaque.DefaultConfiguration()
	material, err := conf.DecodeServerKeyMaterialHex(value)
	if err != nil {
		return "", errors.New("invalid OPAQUE setup")
	}
	defer material.Flush()
	server, err := conf.Server()
	if err != nil {
		return "", errors.New("could not initialize OPAQUE configuration")
	}
	if err = server.SetKeyMaterial(material); err != nil {
		return "", errors.New("invalid OPAQUE server key material")
	}
	return material.Hex(), nil
}

func readSetup(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("setup must be a regular file, not a symlink")
	}
	if info.Size() == 0 || info.Size() > maxSetupBytes {
		return "", errors.New("invalid setup file size; restore the original key")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return "", errors.New("setup file changed during access")
	}
	if opened.Mode().Perm() != 0600 {
		if err = file.Chmod(0600); err != nil {
			return "", fmt.Errorf("enforce private setup permissions: %w", err)
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSetupBytes+1))
	if err != nil {
		return "", err
	}
	defer clear(data)
	if len(data) > maxSetupBytes {
		return "", errors.New("setup file exceeds size limit")
	}
	return validateSetup(string(data))
}

// Publish a completely written and fsynced temporary file without ever
// overwriting a key installed by another process. A crash cannot expose a
// half-written setup under the final name.
func installSetup(dir, path, value string) (bool, error) {
	file, err := os.CreateTemp(dir, ".opaque-server-setup-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return false, err
	}
	if _, err = file.WriteString(value + "\n"); err != nil {
		return false, err
	}
	if err = file.Sync(); err != nil {
		return false, err
	}
	if err = file.Close(); err != nil {
		return false, err
	}
	if err = os.Link(file.Name(), path); errors.Is(err, os.ErrExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return true, err
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return true, err
	}
	return true, nil
}
