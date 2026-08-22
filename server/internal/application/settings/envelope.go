package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// EnvelopeEncryptor is the local envelope-encryption seam for sensitive
// settings. The runtime key is never persisted; each value gets a fresh GCM
// nonce and a versioned JSON envelope so migrations can rotate the algorithm
// without changing the setting API.
type EnvelopeEncryptor struct {
	key [32]byte
}

type envelope struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func NewEnvelopeEncryptor(runtimeKey []byte) (*EnvelopeEncryptor, error) {
	if len(runtimeKey) == 0 {
		return nil, errors.New("settings encryption key is empty")
	}
	// Deriving a fixed-size key keeps the seam compatible with secret-manager
	// values that are not already 32 bytes while ensuring the raw key is never
	// copied into a persisted record.
	return &EnvelopeEncryptor{key: sha256.Sum256(runtimeKey)}, nil
}

func (e *EnvelopeEncryptor) Encrypt(_ context.Context, key string, plaintext []byte) ([]byte, error) {
	if e == nil {
		return nil, errors.New("settings encryptor is not initialized")
	}
	block, err := aes.NewCipher(e.key[:])
	if err != nil {
		return nil, fmt.Errorf("create settings cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create settings envelope: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate settings nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(key))
	return json.Marshal(envelope{
		Version:    1,
		Algorithm:  "AES-256-GCM",
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	})
}

func (e *EnvelopeEncryptor) Decrypt(_ context.Context, key string, encoded []byte) ([]byte, error) {
	if e == nil {
		return nil, errors.New("settings encryptor is not initialized")
	}
	var value envelope
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, errors.New("invalid settings envelope")
	}
	if value.Version != 1 || value.Algorithm != "AES-256-GCM" {
		return nil, errors.New("unsupported settings envelope")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(value.Nonce)
	if err != nil {
		return nil, errors.New("invalid settings envelope nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(value.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid settings envelope ciphertext")
	}
	block, err := aes.NewCipher(e.key[:])
	if err != nil {
		return nil, fmt.Errorf("create settings cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create settings envelope: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(key))
	if err != nil {
		return nil, errors.New("settings envelope authentication failed")
	}
	return plaintext, nil
}

var _ Encryptor = (*EnvelopeEncryptor)(nil)
