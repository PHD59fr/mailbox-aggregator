package filestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func atomicWrite(dir, name string, data []byte, mode os.FileMode) (returnErr error) {
	if filepath.Base(name) != name {
		return ErrInvalidName
	}
	if err := rejectSymlink(dir); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempName := temp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temp.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if removeErr := os.Remove(tempName); returnErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			returnErr = removeErr
		}
	}()
	if err = temp.Chmod(mode); err != nil {
		return err
	}
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	closed = true
	target := filepath.Join(dir, name)
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	if err = os.Rename(tempName, target); err != nil {
		return err
	}
	if err = os.Chmod(target, mode); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
