package verifier

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestPostRestore_SkipsForSqlite(t *testing.T) {
	cfg := &config.Config{}
	cfg.DB.Type = config.DBSQLite
	cfg.DB.Host = "ignored"
	cfg.DB.Port = 5432
	if err := PostRestore(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("expected sqlite to skip, got %v", err)
	}
}

func TestPostRestore_SkipsWhenHostEmpty(t *testing.T) {
	cfg := &config.Config{}
	cfg.DB.Type = config.DBPostgres
	cfg.DB.Host = ""
	cfg.DB.Port = 5432
	if err := PostRestore(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("expected empty host to skip, got %v", err)
	}
}

func TestPostRestore_SkipsWhenPortZero(t *testing.T) {
	cfg := &config.Config{}
	cfg.DB.Type = config.DBPostgres
	cfg.DB.Host = "127.0.0.1"
	cfg.DB.Port = 0
	if err := PostRestore(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("expected port=0 to skip, got %v", err)
	}
}

// TestPostRestore_SuccessfulConnect uses a TCP listener on a random port to
// exercise the happy path. We close the listener after the dial — we only
// care that DialContext returned without error.
func TestPostRestore_SuccessfulConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	cfg := &config.Config{}
	cfg.DB.Type = config.DBPostgres
	cfg.DB.Host = host
	cfg.DB.Port = port

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	if err := PostRestore(context.Background(), cfg, log); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(buf.String(), "target reachable") {
		t.Errorf("expected reachability log line, got:\n%s", buf.String())
	}
}

// TestPostRestore_ConnectFails picks a port on which nothing is listening and
// expects the dial to fail (with a generic timeout/ECONNREFUSED error).
func TestPostRestore_ConnectFails(t *testing.T) {
	// Bind a listener, capture its port, then close it so the port is free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	ln.Close()

	cfg := &config.Config{}
	cfg.DB.Type = config.DBPostgres
	cfg.DB.Host = host
	cfg.DB.Port = port

	if err := PostRestore(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("expected error connecting to closed port, got nil")
	}
}
