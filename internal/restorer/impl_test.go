package restorer

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// installStub writes a shell-script binary into a fresh TempDir or into an existing dir.
func installStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs not portable")
	}
	if dir == "" {
		dir = t.TempDir()
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return dir
}

func baseCfg(t *testing.T, dbType config.DBType) *config.Config {
	return &config.Config{
		DB:      config.DB{Type: dbType, Host: "h", Port: 5432, User: "u", Password: "p"},
		WorkDir: t.TempDir(),
	}
}

func writeGzipSrc(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "src.sql.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	_, _ = gw.Write([]byte(content))
	_ = gw.Close()
	_ = f.Close()
	return p
}

func writeRawSrc(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "src.archive.gz")
	_ = os.WriteFile(p, []byte(content), 0o644)
	return p
}

func TestPostgres_Restore_Success(t *testing.T) {
	d := installStub(t, "", "psql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBPostgres)
	cfg.DB.Name = "appdb"
	r := NewPostgres(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "SELECT 1;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestPostgres_Restore_AllDatabases(t *testing.T) {
	d := installStub(t, "", "psql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBPostgres)
	r := NewPostgres(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "CREATE USER;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestPostgres_Restore_WithCreateDB(t *testing.T) {
	d := installStub(t, "", "psql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBPostgres)
	cfg.DB.Name = "newdb"
	cfg.DB.CreateDB = true
	r := NewPostgres(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "SELECT 1;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestPostgres_Restore_GzipError(t *testing.T) {
	d := installStub(t, "", "psql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBPostgres)
	cfg.DB.Name = "a"
	r := NewPostgres(cfg, discardLogger())

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.sql.gz")
	_ = os.WriteFile(bad, []byte("not gzip"), 0o644)
	if err := r.Restore(context.Background(), bad); err == nil {
		t.Error("expected gzip error")
	}
}

func TestMySQL_Restore_PrefersMySQL(t *testing.T) {
	d := installStub(t, "", "mysql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	cfg.DB.Name = "appdb"
	r := NewMySQL(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMySQL_Restore_FallbackToMariaDB(t *testing.T) {
	d := installStub(t, "", "mariadb", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	cfg.DB.Name = "appdb"
	r := NewMySQL(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMySQL_Restore_NoClient(t *testing.T) {
	d := t.TempDir()
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	r := NewMySQL(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "x")); err == nil {
		t.Error("expected no-client error")
	}
}

func TestMySQL_Restore_CreateDB(t *testing.T) {
	d := installStub(t, "", "mysql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	cfg.DB.Name = "newdb"
	cfg.DB.CreateDB = true
	r := NewMySQL(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMariaDB_Restore_PrefersMariaDB(t *testing.T) {
	d := installStub(t, "", "mariadb", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMariaDB)
	cfg.DB.Port = 3306
	cfg.DB.Name = "appdb"
	r := NewMariaDB(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMariaDB_Restore_FallbackToMySQL(t *testing.T) {
	d := installStub(t, "", "mysql", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMariaDB)
	cfg.DB.Port = 3306
	r := NewMariaDB(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMariaDB_Restore_CreateDB(t *testing.T) {
	d := installStub(t, "", "mariadb", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMariaDB)
	cfg.DB.Port = 3306
	cfg.DB.Name = "newdb"
	cfg.DB.CreateDB = true
	r := NewMariaDB(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeGzipSrc(t, "INSERT;")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMongo_Restore(t *testing.T) {
	d := installStub(t, "", "mongorestore", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMongo)
	cfg.DB.Port = 27017
	cfg.DB.Name = "appdb"
	r := NewMongo(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeRawSrc(t, "mongo-archive-bytes")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}

func TestMongo_Restore_AllDBs(t *testing.T) {
	d := installStub(t, "", "mongorestore", `exit 0`)
	t.Setenv("PATH", d)

	cfg := baseCfg(t, config.DBMongo)
	cfg.DB.Port = 27017
	r := NewMongo(cfg, discardLogger())
	if err := r.Restore(context.Background(), writeRawSrc(t, "bytes")); err != nil {
		t.Errorf("Restore: %v", err)
	}
}
