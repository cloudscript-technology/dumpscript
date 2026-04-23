package verifier

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// etcd snapshot files are BoltDB / bbolt databases. bbolt stores the magic
// constant 0xED0CDAED in meta pages as a host-order uint32, so we accept
// both endian permutations when scanning the first 4 KiB.
var (
	boltMagicLE = []byte{0xED, 0xDA, 0x0C, 0xED}
	boltMagicBE = []byte{0xED, 0x0C, 0xDA, 0xED}
)

func init() {
	Register(config.DBEtcd, func(log *slog.Logger) Verifier { return NewEtcd(log) })
}

type Etcd struct {
	log *slog.Logger
}

func NewEtcd(log *slog.Logger) *Etcd { return &Etcd{log: log} }

func (e *Etcd) Verify(_ context.Context, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("etcd verify open: %w", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("etcd verify gzip: %w", err)
	}
	defer gr.Close()

	var head [4096]byte
	n, err := io.ReadFull(gr, head[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("etcd verify read head: %w", err)
	}
	if !bytes.Contains(head[:n], boltMagicLE) && !bytes.Contains(head[:n], boltMagicBE) {
		return fmt.Errorf("etcd snapshot BoltDB magic not found in first %d bytes; not an etcdctl snapshot or corrupted", n)
	}
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("etcd verify stream: %w", err)
	}
	e.log.Debug("etcd content verified", "path", gzPath)
	return nil
}
