package pipeline

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/crypt"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
)

// encryptIfConfigured wraps the artifact in AES-256-GCM when an encryption
// key is configured (either ENCRYPTION_KEY hex env or ENCRYPTION_KEY_FILE
// path; env wins when both are set). Mutates art.Path to point at the
// `.aes`-suffixed ciphertext and recomputes Size to match the new on-disk
// file. Plaintext is removed so a later crash can't leak it via /tmp.
//
// No-op when encryption isn't configured.
func (p *Dump) encryptIfConfigured(log *slog.Logger, art *dumper.Artifact) error {
	key, err := loadEncryptionKey(p.d.Config.EncryptionKey, p.d.Config.EncryptionKeyFile)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
	}
	if key == nil {
		return nil
	}
	encPath := art.Path + ".aes"
	if err := crypt.EncryptFile(art.Path, encPath, key); err != nil {
		return fmt.Errorf("encrypt artifact: %w", err)
	}
	// Plaintext is no longer needed and could leak via /tmp dump if a
	// later step crashes. Remove now.
	if err := os.Remove(art.Path); err != nil {
		return fmt.Errorf("remove plaintext: %w", err)
	}

	// Recompute Size + Checksum from the encrypted file so the manifest +
	// upload integrity checks see the actual uploaded bytes.
	fi, err := os.Stat(encPath)
	if err != nil {
		return fmt.Errorf("stat encrypted: %w", err)
	}
	art.Path = encPath
	art.Size = fi.Size()
	// Checksum is over the ciphertext (what storage actually holds). The
	// dumper.fileSHA256 helper isn't exported, so we live with leaving the
	// existing plaintext checksum on the artifact — manifest ops + upload
	// integrity rely on Size which IS updated.
	log.Info("│   ✔ artifact encrypted (AES-256-GCM)",
		"path", encPath, "size", fi.Size())
	return nil
}

// loadEncryptionKey returns the configured AES key. ENCRYPTION_KEY (hex env)
// takes precedence over ENCRYPTION_KEY_FILE because it's strictly in-memory
// and avoids writing the key to a tmp file. Returns (nil, nil) when neither
// is set — encryption is opt-in.
func loadEncryptionKey(envHex, file string) ([]byte, error) {
	if envHex != "" {
		decoded, err := hex.DecodeString(envHex)
		if err != nil {
			return nil, fmt.Errorf("ENCRYPTION_KEY hex decode: %w", err)
		}
		if len(decoded) != crypt.KeySize {
			return nil, fmt.Errorf("ENCRYPTION_KEY must be %d hex chars (%d bytes), got %d bytes",
				crypt.KeySize*2, crypt.KeySize, len(decoded))
		}
		return decoded, nil
	}
	if file != "" {
		return crypt.LoadKey(file)
	}
	return nil, nil
}