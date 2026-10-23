package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/gin-gonic/gin"
)

// References hide folder names from URLs and logs. They are randomized,
// authenticated, bound to the HTTP session and account, and never persisted.
// Authorization still comes from that session's activated IMAP credentials.
func folderCipher(token string) (cipher.AEAD, error) {
	if token == "" {
		return nil, errors.New("missing session")
	}
	key := sha256.Sum256([]byte("mailbox-folder-reference-v1\x00" + token))
	block, err := aes.NewCipher(key[:])
	clear(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encodeFolderReference(token, account, name string) (string, error) {
	if !imap.ValidFolderName(name) {
		return "", errors.New("invalid folder")
	}
	aead, err := folderCipher(token)
	if err != nil {
		return "", err
	}
	// Bucket padding hides the exact length of ordinary short folder names.
	plain := make([]byte, ((len(name)+2+31)/32)*32)
	defer clear(plain)
	binary.BigEndian.PutUint16(plain, uint16(len(name)))
	copy(plain[2:], name)
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(account))
	return "f1_" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decodeFolderReference(token, account, id string) (string, error) {
	invalid := errors.New("invalid or expired folder reference")
	if !strings.HasPrefix(id, "f1_") || len(id) > 1500 {
		return "", invalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "f1_"))
	if err != nil {
		return "", invalid
	}
	aead, err := folderCipher(token)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead()+2 {
		return "", invalid
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(account))
	if err != nil {
		return "", invalid
	}
	defer clear(plain)
	size := int(binary.BigEndian.Uint16(plain))
	if size > len(plain)-2 {
		return "", invalid
	}
	name := string(plain[2 : 2+size])
	if !imap.ValidFolderName(name) {
		return "", invalid
	}
	return name, nil
}

func (h *handlers) folderReference(c *gin.Context) {
	var input struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&input) != nil || !imap.ValidFolderName(input.Name) {
		respondError(c, 400, "INVALID_INPUT", "Choose a valid folder.")
		return
	}
	h.mailboxOperation(c, func(_ context.Context, _ imap.Configuration) {
		id, err := encodeFolderReference(c.GetString(contextToken), c.Param("account"), input.Name)
		if err != nil {
			respondError(c, 500, "FOLDER_REFERENCE_FAILED", "Could not prepare the folder reference.")
			return
		}
		c.JSON(200, gin.H{"id": id})
	})
}
