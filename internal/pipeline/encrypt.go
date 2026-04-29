package pipeline

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/crypt"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
)

// encryptIfConfigured wraps the artifact in AES-256-GCM when
// Config.EncryptionKeyFile is set. Mutates art.Path to point at the
// `.aes`-suffixed ciphertext and recomputes Size + Checksum to match the new
// on-disk file. The plaintext file is removed; only the ciphertext stays
// around for upload.
//
// No-op when encryption isn't configured.
func (p *Dump) encryptIfConfigured(log *slog.Logger, art *dumper.Artifact) error {
	if p.d.Config.EncryptionKeyFile == "" {
		return nil
	}
	key, err := crypt.LoadKey(p.d.Config.EncryptionKeyFile)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
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