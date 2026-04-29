// Package crypt provides AES-256-GCM file encryption and decryption used by
// the dump/restore pipelines for client-side at-rest protection.
//
// Format on disk:
//
//	[12-byte nonce][ciphertext+GCM tag]
//
// GCM provides AEAD: tampering with the ciphertext or nonce is detected on
// decryption and surfaces as an error rather than corrupted output. The
// per-file nonce is generated from crypto/rand; with a 32-byte key the same
// key can safely encrypt 2^96 unique files before nonce reuse becomes a
// concern (more than enough for backup workloads).
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// KeySize is the required AES-256 key length in bytes.
const KeySize = 32

// LoadKey reads an AES-256 key from path. The key may be either:
//   - 32 raw bytes (binary file)
//   - 64-character hex-encoded ASCII (with optional trailing newline / whitespace)
//
// Other lengths return an error so a misconfigured key is caught at startup
// instead of silently producing weak ciphertext.
func LoadKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file %s: %w", path, err)
	}
	// Try hex first — strip ASCII whitespace and decode.
	trimmed := strings.TrimSpace(string(raw))
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	// Fall back to raw bytes.
	if len(raw) == KeySize {
		return raw, nil
	}
	return nil, fmt.Errorf("encryption key must be %d bytes (raw) or %d hex chars; got %d bytes",
		KeySize, KeySize*2, len(raw))
}

// EncryptFile reads src, encrypts with AES-256-GCM under key, and writes the
// result to dst. dst is created with mode 0600 to match the secret-handling
// posture of the rest of the binary. On any error the partial dst is removed.
func EncryptFile(src, dst string, key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
	}
	plaintext, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read src: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("read random nonce: %w", err)
	}
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	if err := os.WriteFile(dst, out, 0o600); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("write dst: %w", err)
	}
	return nil
}

// DecryptFile reads src (encrypted), decrypts with AES-256-GCM under key, and
// writes plaintext to dst (mode 0600). Tampering surfaces as a non-nil error
// — dst is removed in that case so the caller can never accidentally feed
// corrupt content into a restore.
func DecryptFile(src, dst string, key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
	}
	enc, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read src: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("cipher.NewGCM: %w", err)
	}
	if len(enc) < gcm.NonceSize() {
		return errors.New("ciphertext shorter than GCM nonce — file corrupt or wrong format")
	}
	nonce, ciphertext := enc[:gcm.NonceSize()], enc[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("gcm.Open (auth tag mismatch — wrong key or tampered file): %w", err)
	}
	if err := os.WriteFile(dst, plaintext, 0o600); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("write dst: %w", err)
	}
	return nil
}
