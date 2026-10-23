package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	MetadataVersion = 1
	FileVersion     = 1
)

type Argon2Parameters struct {
	MemoryKiB   uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"key_length"`
}

type Metadata struct {
	Version       int              `json:"version"`
	KDF           string           `json:"kdf"`
	KDFSalt       string           `json:"kdf_salt"`
	KDFParameters Argon2Parameters `json:"kdf_parameters"`
	WrapAlgorithm string           `json:"wrap_algorithm"`
	WrappedVMK    string           `json:"wrapped_vmk"`
	WrapNonce     string           `json:"wrap_nonce"`
	KeyVersion    uint32           `json:"key_version"`
}

type EncryptedFile struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	KeyVersion uint32 `json:"key_version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func ValidateMetadataJSON(data []byte) error {
	var value Metadata
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	if value.Version != MetadataVersion || value.KDF != "argon2id" || value.WrapAlgorithm != "AES-256-GCM" || value.KeyVersion == 0 {
		return errors.New("unsupported vault metadata")
	}
	if value.KDFParameters.MemoryKiB < 64*1024 || value.KDFParameters.MemoryKiB > 1024*1024 || value.KDFParameters.Iterations < 2 || value.KDFParameters.Iterations > 10 || value.KDFParameters.Parallelism < 1 || value.KDFParameters.Parallelism > 8 || value.KDFParameters.KeyLength != 32 {
		return errors.New("unsafe Argon2id parameters")
	}
	if err := exactBase64(value.KDFSalt, 16); err != nil {
		return fmt.Errorf("invalid KDF salt: %w", err)
	}
	if err := exactBase64(value.WrapNonce, 12); err != nil {
		return fmt.Errorf("invalid wrap nonce: %w", err)
	}
	wrapped, err := base64.RawStdEncoding.DecodeString(value.WrappedVMK)
	if err != nil || len(wrapped) != 48 {
		return errors.New("invalid wrapped VMK")
	}
	return nil
}

func ValidateEncryptedFileJSON(data []byte) error {
	var value EncryptedFile
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	if value.Version != FileVersion || value.Algorithm != "AES-256-GCM" || value.KeyVersion == 0 {
		return errors.New("unsupported encrypted file format")
	}
	if err := exactBase64(value.Nonce, 12); err != nil {
		return fmt.Errorf("invalid nonce: %w", err)
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(value.Ciphertext)
	if err != nil || len(ciphertext) < 16 {
		return errors.New("invalid ciphertext")
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("trailing data after JSON: %w", err)
	}
	return errors.New("unexpected trailing JSON")
}
func exactBase64(value string, length int) error {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	if len(decoded) != length {
		return fmt.Errorf("expected %d bytes", length)
	}
	return nil
}
