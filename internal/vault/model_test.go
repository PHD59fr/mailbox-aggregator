package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func b64(size int) string {
	value := make([]byte, size)
	_, _ = rand.Read(value)
	return base64.RawStdEncoding.EncodeToString(value)
}

func TestValidateMetadataAndEncryptedFile(t *testing.T) {
	metadata, _ := json.Marshal(Metadata{Version: 1, KDF: "argon2id", KDFSalt: b64(16), KDFParameters: Argon2Parameters{MemoryKiB: 65536, Iterations: 3, Parallelism: 1, KeyLength: 32}, WrapAlgorithm: "AES-256-GCM", WrappedVMK: b64(48), WrapNonce: b64(12), KeyVersion: 1})
	if err := ValidateMetadataJSON(metadata); err != nil {
		t.Fatal(err)
	}
	file, _ := json.Marshal(EncryptedFile{Version: 1, Algorithm: "AES-256-GCM", KeyVersion: 1, Nonce: b64(12), Ciphertext: b64(17)})
	if err := ValidateEncryptedFileJSON(file); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(file, &parsed)
	parsed["ciphertext"] = b64(15)
	bad, _ := json.Marshal(parsed)
	if err := ValidateEncryptedFileJSON(bad); err == nil {
		t.Fatal("short ciphertext accepted")
	}
	parsed["ciphertext"] = b64(17)
	parsed["unknown"] = true
	bad, _ = json.Marshal(parsed)
	if err := ValidateEncryptedFileJSON(bad); err == nil {
		t.Fatal("unknown field accepted")
	}
}
