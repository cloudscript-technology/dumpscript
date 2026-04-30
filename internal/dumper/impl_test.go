package dumper

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// installStub writes a shell-script binary named `name` into a fresh temp dir
// and prepends that dir to PATH. Returns the stub dir.
func installStub(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs not portable to Windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func baseCfg(t *testing.T, dbType config.DBType) *config.Config {
	return &config.Config{
		DB:      config.DB{Type: dbType, Host: "h", Port: 5432, User: "u", Password: "p"},
		WorkDir: t.TempDir(),
	}
}

func readGzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gr.Close()
	buf, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestPostgres_Dump_WithDBName(t *testing.T) {
	installStub(t, "pg_dump", `echo "SELECT 1;"`)

	cfg := baseCfg(t, config.DBPostgres)
	cfg.DB.Name = "appdb"
	d := NewPostgres(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()

	if art.Extension != "sql" {
		t.Errorf("extension = %s", art.Extension)
	}
	content := readGzip(t, art.Path)
	if content != "SELECT 1;\n" {
		t.Errorf("content = %q", content)
	}
}

func TestPostgres_Dump_WithoutDBName_UsesDumpall(t *testing.T) {
	installStub(t, "pg_dumpall", `echo "CREATE USER;"`)

	cfg := baseCfg(t, config.DBPostgres)
	cfg.DB.Name = "" // pg_dumpall
	d := NewPostgres(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if content := readGzip(t, art.Path); content != "CREATE USER;\n" {
		t.Errorf("content = %q — stub for pg_dumpall may not have been invoked", content)
	}
}

func TestMySQL_Dump_Success(t *testing.T) {
	installStub(t, "mysqldump", `echo "-- mysql dump"`)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	cfg.DB.Name = "appdb"
	d := NewMySQL(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if content := readGzip(t, art.Path); content != "-- mysql dump\n" {
		t.Errorf("content = %q", content)
	}
}

func TestMySQL_Dump_AllDatabases(t *testing.T) {
	installStub(t, "mysqldump", `echo "ALL DBS"`)
	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	d := NewMySQL(cfg, discardLogger())
	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if content := readGzip(t, art.Path); content != "ALL DBS\n" {
		t.Errorf("content = %q", content)
	}
}

func TestMySQL_Dump_MySQL57_FallbackToMariaDBDump(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mariadb-dump")
	_ = os.WriteFile(p, []byte("#!/bin/sh\necho \"fallback\"\n"), 0o755)
	t.Setenv("PATH", dir) // exclusive, so mysqldump is not found

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.MySQLVersion = "5.7"
	cfg.DB.Port = 3306
	cfg.DB.Name = "app"
	d := NewMySQL(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if content := readGzip(t, art.Path); content != "fallback\n" {
		t.Errorf("content = %q", content)
	}
}

func TestMySQL_Dump_NoClientAvailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	cfg := baseCfg(t, config.DBMySQL)
	cfg.DB.Port = 3306
	d := NewMySQL(cfg, discardLogger())
	_, err := d.Dump(context.Background())
	if err == nil {
		t.Fatal("expected error when no client available")
	}
}

func TestMariaDB_Dump(t *testing.T) {
	installStub(t, "mariadb-dump", `echo "-- mariadb dump"`)

	cfg := baseCfg(t, config.DBMariaDB)
	cfg.DB.Port = 3306
	cfg.DB.Name = "appdb"
	d := NewMariaDB(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if content := readGzip(t, art.Path); content != "-- mariadb dump\n" {
		t.Errorf("content = %q", content)
	}
}

func TestMongo_Dump(t *testing.T) {
	// mongodump emits gzip on stdout. Stub pipes through `gzip -c`.
	installStub(t, "mongodump", `echo "mongo-archive" | gzip -c`)

	cfg := baseCfg(t, config.DBMongo)
	cfg.DB.Port = 27017
	cfg.DB.Name = "app"
	d := NewMongo(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "archive" {
		t.Errorf("extension = %s", art.Extension)
	}
	if content := readGzip(t, art.Path); content != "mongo-archive\n" {
		t.Errorf("content = %q", content)
	}
}
