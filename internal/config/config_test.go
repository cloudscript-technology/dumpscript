package config

import (
	"strings"
	"testing"
)

func TestApplyDefaults_PortPerDBType(t *testing.T) {
	tests := []struct {
		t    DBType
		port int
	}{
		{DBPostgres, 5432},
		{DBMySQL, 3306},
		{DBMariaDB, 3306},
		{DBMongo, 27017},
	}
	for _, tc := range tests {
		t.Run(string(tc.t), func(t *testing.T) {
			c := &Config{DB: DB{Type: tc.t}}
			c.applyDefaults()
			if c.DB.Port != tc.port {
				t.Errorf("port = %d, want %d", c.DB.Port, tc.port)
			}
		})
	}
}

func TestApplyDefaults_DoesNotOverridePort(t *testing.T) {
	c := &Config{DB: DB{Type: DBPostgres, Port: 9999}}
	c.applyDefaults()
	if c.DB.Port != 9999 {
		t.Errorf("port was overridden: %d", c.DB.Port)
	}
}

func TestApplyDefaults_AzurePrefixFallback(t *testing.T) {
	c := &Config{
		Backend: BackendAzure,
		S3:      S3{Prefix: "legacy"},
	}
	c.applyDefaults()
	if c.Azure.Prefix != "legacy" {
		t.Errorf("Azure.Prefix = %q, want legacy", c.Azure.Prefix)
	}
}

func TestApplyDefaults_AzurePrefixExplicitWins(t *testing.T) {
	c := &Config{
		Backend: BackendAzure,
		S3:      S3{Prefix: "legacy"},
		Azure:   Azure{Prefix: "az"},
	}
	c.applyDefaults()
	if c.Azure.Prefix != "az" {
		t.Errorf("explicit Azure.Prefix was overridden: %q", c.Azure.Prefix)
	}
}

func TestContainerAndPrefix(t *testing.T) {
	s3 := &Config{Backend: BackendS3, S3: S3{Bucket: "b", Prefix: "p"}}
	if s3.Container() != "b" {
		t.Errorf("s3 container = %q", s3.Container())
	}
	if s3.Prefix() != "p" {
		t.Errorf("s3 prefix = %q", s3.Prefix())
	}
	az := &Config{Backend: BackendAzure, Azure: Azure{Container: "c", Prefix: "ap"}}
	if az.Container() != "c" {
		t.Errorf("azure container = %q", az.Container())
	}
	if az.Prefix() != "ap" {
		t.Errorf("azure prefix = %q", az.Prefix())
	}
}

func baseValid(dbType DBType) Config {
	return Config{
		DB:          DB{Type: dbType, Host: "h", User: "u", Password: "p"},
		S3:          S3{Bucket: "b"},
		Backend:     BackendS3,
		Periodicity: Daily,
	}
}

func TestValidateCommon(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"ok", baseValid(DBPostgres), ""},
		{"missing DB_TYPE", func() Config { c := baseValid(DBPostgres); c.DB.Type = ""; return c }(), "DB_TYPE is required"},
		{"bad DB_TYPE", func() Config { c := baseValid(DBPostgres); c.DB.Type = "not-a-real-db"; return c }(), "DB_TYPE must be"},
		{"bad backend", func() Config { c := baseValid(DBPostgres); c.Backend = "foo"; return c }(), "unknown STORAGE_BACKEND"},
		{"missing backend", func() Config { c := baseValid(DBPostgres); c.Backend = ""; return c }(), "STORAGE_BACKEND is required"},
		{"s3 no bucket", func() Config { c := baseValid(DBPostgres); c.S3.Bucket = ""; return c }(), "S3_BUCKET required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateCommon()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateCommon_AzureBranches(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"azure ok", Config{
			DB: DB{Type: DBPostgres}, Backend: BackendAzure,
			Azure: Azure{Account: "a", Key: "k", Container: "c"},
		}, ""},
		{"azure no account", Config{
			DB: DB{Type: DBPostgres}, Backend: BackendAzure,
			Azure: Azure{Key: "k", Container: "c"},
		}, "AZURE_STORAGE_ACCOUNT required"},
		{"azure no key/sas", Config{
			DB: DB{Type: DBPostgres}, Backend: BackendAzure,
			Azure: Azure{Account: "a", Container: "c"},
		}, "AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN"},
		{"azure no container", Config{
			DB: DB{Type: DBPostgres}, Backend: BackendAzure,
			Azure: Azure{Account: "a", Key: "k"},
		}, "AZURE_STORAGE_CONTAINER required"},
		{"azure with SAS", Config{
			DB: DB{Type: DBPostgres}, Backend: BackendAzure,
			Azure: Azure{Account: "a", SASToken: "tok", Container: "c"},
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateCommon()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateDump(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"ok", func(*Config) {}, ""},
		{"missing periodicity", func(c *Config) { c.Periodicity = "" }, "PERIODICITY is required"},
		{"bad periodicity", func(c *Config) { c.Periodicity = "hourly" }, "PERIODICITY must be"},
		{"missing host", func(c *Config) { c.DB.Host = "" }, "DB_HOST is required"},
		{"missing user", func(c *Config) { c.DB.User = "" }, "DB_USER is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := baseValid(DBPostgres)
			tc.mutate(&c)
			err := c.ValidateDump()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateRestore(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		c := baseValid(DBPostgres)
		c.Periodicity = ""
		c.S3.Key = "some/key.sql.gz"
		if err := c.ValidateRestore(); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})
	t.Run("missing S3_KEY", func(t *testing.T) {
		c := baseValid(DBPostgres)
		c.Periodicity = ""
		if err := c.ValidateRestore(); err == nil || !strings.Contains(err.Error(), "S3_KEY") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("missing host", func(t *testing.T) {
		c := baseValid(DBPostgres)
		c.Periodicity = ""
		c.S3.Key = "k"
		c.DB.Host = ""
		if err := c.ValidateRestore(); err == nil || !strings.Contains(err.Error(), "DB_HOST") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLoad_AppliesPortDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "postgresql")
	t.Setenv("DB_HOST", "h")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("PERIODICITY", "daily")
	t.Setenv("STORAGE_BACKEND", "s3")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DB.Port != 5432 {
		t.Errorf("expected default port 5432, got %d", c.DB.Port)
	}
	if c.Backend != BackendS3 {
		t.Errorf("backend = %q", c.Backend)
	}
	if c.Upload.ChunkSize != "100M" || c.Upload.Cutoff != "200M" || c.Upload.Concurrency != 4 {
		t.Errorf("upload defaults wrong: %+v", c.Upload)
	}
	if c.WorkDir != "/dumpscript" {
		t.Errorf("workdir = %q", c.WorkDir)
	}
}
