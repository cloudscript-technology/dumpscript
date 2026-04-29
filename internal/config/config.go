// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type DBType string

const (
	DBPostgres      DBType = "postgresql"
	DBMySQL         DBType = "mysql"
	DBMariaDB       DBType = "mariadb"
	DBMongo         DBType = "mongodb"
	DBCockroach     DBType = "cockroach"
	DBRedis         DBType = "redis"
	DBSQLServer     DBType = "sqlserver"
	DBOracle        DBType = "oracle"
	DBElasticsearch DBType = "elasticsearch"
	DBEtcd          DBType = "etcd"
	DBClickhouse    DBType = "clickhouse"
	DBNeo4j         DBType = "neo4j"
	DBSQLite        DBType = "sqlite"
)

type StorageBackend string

const (
	BackendS3    StorageBackend = "s3"
	BackendAzure StorageBackend = "azure"
	BackendGCS   StorageBackend = "gcs"
)

type Periodicity string

const (
	Daily   Periodicity = "daily"
	Weekly  Periodicity = "weekly"
	Monthly Periodicity = "monthly"
	Yearly  Periodicity = "yearly"
)

type DB struct {
	Type        DBType `envconfig:"DB_TYPE"`
	Host        string `envconfig:"DB_HOST"`
	Port        int    `envconfig:"DB_PORT"`
	User        string `envconfig:"DB_USER"`
	Password    string `envconfig:"DB_PASSWORD"`
	Name        string `envconfig:"DB_NAME"`
	DumpOptions string `envconfig:"DUMP_OPTIONS"`
	CreateDB    bool   `envconfig:"CREATE_DB"`

	PostgresVersion string `envconfig:"POSTGRES_VERSION" default:"16"`
	MySQLVersion    string `envconfig:"MYSQL_VERSION" default:"8.0"`
	MariaDBVersion  string `envconfig:"MARIADB_VERSION" default:"11.4"`
}

type S3 struct {
	Region          string `envconfig:"AWS_REGION"`
	Bucket          string `envconfig:"S3_BUCKET"`
	Prefix          string `envconfig:"S3_PREFIX"`
	AccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID"`
	SecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY"`
	SessionToken    string `envconfig:"AWS_SESSION_TOKEN"`
	RoleARN         string `envconfig:"AWS_ROLE_ARN"`
	EndpointURL     string `envconfig:"AWS_S3_ENDPOINT_URL"`
	StorageClass    string `envconfig:"S3_STORAGE_CLASS"`
	Key             string `envconfig:"S3_KEY"`

	// SSE — server-side encryption algorithm. One of "" (none), "AES256",
	// "aws:kms". When set to "aws:kms", SSEKMSKeyID may be set; if empty,
	// the bucket's default KMS key is used.
	SSE         string `envconfig:"S3_SSE"`
	SSEKMSKeyID string `envconfig:"S3_SSE_KMS_KEY_ID"`
}

type Azure struct {
	Account   string `envconfig:"AZURE_STORAGE_ACCOUNT"`
	Key       string `envconfig:"AZURE_STORAGE_KEY"`
	SASToken  string `envconfig:"AZURE_STORAGE_SAS_TOKEN"`
	Container string `envconfig:"AZURE_STORAGE_CONTAINER"`
	Prefix    string `envconfig:"AZURE_STORAGE_PREFIX"`
	// Endpoint overrides the default https://<account>.blob.core.windows.net.
	// Used to target Azurite emulator or custom Azure government clouds.
	// Example: http://azurite:10000/devstoreaccount1
	Endpoint string `envconfig:"AZURE_STORAGE_ENDPOINT"`
}

// GCS native (Google Cloud Storage) — uses Application Default Credentials.
// On GKE this means Workload Identity (no static keys); locally it uses
// `gcloud auth application-default login` or GOOGLE_APPLICATION_CREDENTIALS.
type GCS struct {
	Bucket          string `envconfig:"GCS_BUCKET"`
	Prefix          string `envconfig:"GCS_PREFIX"`
	ProjectID       string `envconfig:"GCS_PROJECT_ID"`
	CredentialsFile string `envconfig:"GCS_CREDENTIALS_FILE"`
	// Endpoint overrides the default Google API URL — used to point at the
	// fake-gcs-server emulator for local development and tests
	// (e.g. http://localhost:4443/storage/v1/).
	Endpoint string `envconfig:"GCS_ENDPOINT"`
}

type Upload struct {
	Cutoff      string `envconfig:"STORAGE_UPLOAD_CUTOFF" default:"200M"`
	ChunkSize   string `envconfig:"STORAGE_CHUNK_SIZE"    default:"100M"`
	Concurrency int    `envconfig:"STORAGE_UPLOAD_CONCURRENCY" default:"4"`
}

type Slack struct {
	WebhookURL    string `envconfig:"SLACK_WEBHOOK_URL"`
	Channel       string `envconfig:"SLACK_CHANNEL"`
	Username      string `envconfig:"SLACK_USERNAME"`
	NotifySuccess bool   `envconfig:"SLACK_NOTIFY_SUCCESS" default:"false"`
}

// Discord posts via an Incoming Webhook URL.
type Discord struct {
	WebhookURL    string `envconfig:"DISCORD_WEBHOOK_URL"`
	Username      string `envconfig:"DISCORD_USERNAME"`
	NotifySuccess bool   `envconfig:"DISCORD_NOTIFY_SUCCESS" default:"false"`
}

// Teams posts via a Microsoft Teams Incoming Webhook (legacy connector).
type Teams struct {
	WebhookURL    string `envconfig:"TEAMS_WEBHOOK_URL"`
	NotifySuccess bool   `envconfig:"TEAMS_NOTIFY_SUCCESS" default:"false"`
}

// Webhook is a generic JSON POST receiver — any HTTP server that accepts
// `application/json`. AuthHeader is the raw value of the Authorization
// header sent on every request (e.g. `Bearer xxx`).
type Webhook struct {
	URL           string `envconfig:"WEBHOOK_URL"`
	AuthHeader    string `envconfig:"WEBHOOK_AUTH_HEADER"`
	NotifySuccess bool   `envconfig:"WEBHOOK_NOTIFY_SUCCESS" default:"false"`
}

// NotifyStdout emits each event as JSON on stdout — useful for log-based
// downstream tooling (CI dashboards, fluent-bit, etc.).
type NotifyStdout struct {
	Enabled       bool `envconfig:"NOTIFY_STDOUT"         default:"false"`
	NotifySuccess bool `envconfig:"NOTIFY_STDOUT_SUCCESS" default:"true"`
}

type Prometheus struct {
	Enabled        bool   `envconfig:"PROMETHEUS_ENABLED"         default:"false"`
	PushgatewayURL string `envconfig:"PROMETHEUS_PUSHGATEWAY_URL"`
	JobName        string `envconfig:"PROMETHEUS_JOB_NAME"        default:"dumpscript"`
	Instance       string `envconfig:"PROMETHEUS_INSTANCE"`
	LogOnExit      bool   `envconfig:"PROMETHEUS_LOG_ON_EXIT"     default:"false"`
}

type Config struct {
	DB           DB
	S3           S3
	Azure        Azure
	GCS          GCS
	Upload       Upload
	Slack        Slack
	Discord      Discord
	Teams        Teams
	Webhook      Webhook
	NotifyStdout NotifyStdout
	Prometheus   Prometheus

	Backend        StorageBackend `envconfig:"STORAGE_BACKEND" default:"s3"`
	Periodicity    Periodicity    `envconfig:"PERIODICITY"`
	RetentionDays  int            `envconfig:"RETENTION_DAYS"`
	WorkDir        string         `envconfig:"WORK_DIR"        default:"/dumpscript"`
	LogLevel       string         `envconfig:"LOG_LEVEL"       default:"info"`
	LogFormat      string         `envconfig:"LOG_FORMAT"      default:"json"`
	VerifyContent  bool           `envconfig:"VERIFY_CONTENT"  default:"true"`
	DumpTimeout    time.Duration  `envconfig:"DUMP_TIMEOUT"    default:"2h"`
	RestoreTimeout time.Duration  `envconfig:"RESTORE_TIMEOUT" default:"2h"`

	// DumpRetries — how many times to retry a failed dump (1 = single attempt).
	// Useful when the source DB is mid-failover or the network is flaky.
	DumpRetries        int           `envconfig:"DUMP_RETRIES"          default:"3"`
	DumpRetryBackoff   time.Duration `envconfig:"DUMP_RETRY_BACKOFF"    default:"5s"`
	DumpRetryMaxBackoff time.Duration `envconfig:"DUMP_RETRY_MAX_BACKOFF" default:"5m"`

	// DryRun — when true, the dump pipeline validates configuration and
	// connectivity but skips the actual database dump and storage upload.
	DryRun bool `envconfig:"DRY_RUN" default:"false"`

	// CompressionType — gzip (default, backwards-compatible) or zstd.
	CompressionType string `envconfig:"COMPRESSION_TYPE" default:"gzip"`

	// LockGracePeriod — when a stale lock exists in storage and is older than
	// this, dumpscript overwrites it instead of failing. 0 disables stale-lock
	// recovery (current behavior).
	LockGracePeriod time.Duration `envconfig:"LOCK_GRACE_PERIOD" default:"24h"`

	// MetricsListen — when set (e.g. ":9090"), the binary spawns an HTTP
	// listener on that address serving promhttp.Handler() at /metrics.
	// Useful for daemon-mode deployments; CronJobs can leave it empty and
	// rely on the operator's own metrics endpoint.
	MetricsListen string `envconfig:"METRICS_LISTEN" default:""`

	// PostDumpHook — when set, the binary executes this command after a
	// successful dump+upload+manifest, with the run's metadata exposed as
	// environment variables (DUMPSCRIPT_KEY, DUMPSCRIPT_SIZE, etc).
	// Use cases: update an external catalog, trigger downstream rotation,
	// page on success, etc. The hook runs with a 1-minute timeout and its
	// failure is logged as Warn but does NOT fail the pipeline (the dump
	// itself is the authoritative artifact).
	PostDumpHook        string        `envconfig:"POST_DUMP_HOOK"`
	PostDumpHookTimeout time.Duration `envconfig:"POST_DUMP_HOOK_TIMEOUT" default:"60s"`

	// EncryptionKeyFile — path to a 32-byte AES key (hex-encoded, 64 ASCII
	// chars). When set, the dumper encrypts the compressed artifact with
	// AES-256-GCM in-place before upload. Resulting object suffix becomes
	// `.aes`. Restore reverses the process by decrypting before piping
	// into the engine's restorer. Storage-side encryption (SSE-KMS) defends
	// against provider-account leaks; client-side encryption defends
	// additionally against the storage admin reading the bytes.
	EncryptionKeyFile string `envconfig:"ENCRYPTION_KEY_FILE"`

	// EncryptionKey — alternative to EncryptionKeyFile: the 64-character
	// hex-encoded AES key passed directly. Useful in environments where
	// mounting a Secret as a file is awkward (kind-e2e tests, ad-hoc
	// runs). Production deployments should prefer the *File path so the
	// raw key never sits in `kubectl describe pod` output.
	// When both are set, EncryptionKey wins (in-memory, never serialised).
	EncryptionKey string `envconfig:"ENCRYPTION_KEY"`
}

// Load reads the configuration from environment variables.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("envconfig: %w", err)
	}
	c.applyDefaults()
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.DB.Port == 0 {
		switch c.DB.Type {
		case DBPostgres:
			c.DB.Port = 5432
		case DBMySQL, DBMariaDB:
			c.DB.Port = 3306
		case DBMongo:
			c.DB.Port = 27017
		case DBCockroach:
			c.DB.Port = 26257
		case DBRedis:
			c.DB.Port = 6379
		case DBSQLServer:
			c.DB.Port = 1433
		case DBOracle:
			c.DB.Port = 1521
		case DBElasticsearch:
			c.DB.Port = 9200
		case DBEtcd:
			c.DB.Port = 2379
		case DBClickhouse:
			c.DB.Port = 9000
		case DBNeo4j:
			c.DB.Port = 7687
		}
	}
	// Legacy compat: when Azure is active but only S3_PREFIX was set.
	if c.Backend == BackendAzure && c.Azure.Prefix == "" {
		c.Azure.Prefix = c.S3.Prefix
	}
	// Same convenience for GCS — let users keep `S3_PREFIX` from a previous
	// backend and just flip STORAGE_BACKEND=gcs without re-keying everything.
	if c.Backend == BackendGCS && c.GCS.Prefix == "" {
		c.GCS.Prefix = c.S3.Prefix
	}
}

// Container returns the bucket/container for the active backend.
func (c *Config) Container() string {
	switch c.Backend {
	case BackendAzure:
		return c.Azure.Container
	case BackendGCS:
		return c.GCS.Bucket
	}
	return c.S3.Bucket
}

// Prefix returns the storage prefix for the active backend.
func (c *Config) Prefix() string {
	switch c.Backend {
	case BackendAzure:
		return c.Azure.Prefix
	case BackendGCS:
		return c.GCS.Prefix
	}
	return c.S3.Prefix
}

// ValidateCommon validates fields required for every subcommand.
func (c *Config) ValidateCommon() error {
	var errs []string
	switch c.DB.Type {
	case DBPostgres, DBMySQL, DBMariaDB, DBMongo,
		DBCockroach, DBRedis, DBSQLServer, DBOracle,
		DBElasticsearch, DBEtcd, DBClickhouse, DBNeo4j, DBSQLite:
	case "":
		errs = append(errs, "DB_TYPE is required")
	default:
		errs = append(errs, fmt.Sprintf(
			"DB_TYPE must be one of postgresql|mysql|mariadb|mongodb|cockroach|redis|sqlserver|oracle|elasticsearch|etcd|clickhouse|neo4j|sqlite (got %q)",
			c.DB.Type))
	}
	if err := c.validateBackend(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateDump adds dump-specific checks.
func (c *Config) ValidateDump() error {
	if err := c.ValidateCommon(); err != nil {
		return err
	}
	switch c.Periodicity {
	case Daily, Weekly, Monthly, Yearly:
	case "":
		return errors.New("PERIODICITY is required (daily|weekly|monthly|yearly)")
	default:
		return fmt.Errorf("PERIODICITY must be daily|weekly|monthly|yearly (got %q)", c.Periodicity)
	}
	return c.validateConnection()
}

// ValidateRestore adds restore-specific checks.
func (c *Config) ValidateRestore() error {
	if err := c.ValidateCommon(); err != nil {
		return err
	}
	if c.S3.Key == "" {
		return errors.New("S3_KEY is required for restore (storage object key)")
	}
	return c.validateConnection()
}

// ValidateConnection enforces the per-engine connection requirements. Public
// so the `dumpscript validate` subcommand can call it without re-implementing
// the rules. Used internally by ValidateDump/ValidateRestore as well.
//
//   - SQLite is file-based: DB_NAME holds the path, no host/user.
//   - Redis / etcd / Elasticsearch accept anonymous access — DB_USER is
//     optional; DB_HOST is still required.
//   - Every other engine requires both DB_HOST and DB_USER.
func (c *Config) ValidateConnection() error { return c.validateConnection() }

func (c *Config) validateConnection() error {
	if c.DB.Type == DBSQLite {
		if c.DB.Name == "" {
			return errors.New("DB_NAME (path to .sqlite file) is required")
		}
		return nil
	}
	if c.DB.Host == "" {
		return errors.New("DB_HOST is required")
	}
	switch c.DB.Type {
	case DBRedis, DBEtcd, DBElasticsearch:
		return nil
	}
	if c.DB.User == "" {
		return errors.New("DB_USER is required")
	}
	return nil
}

func (c *Config) validateBackend() error {
	switch c.Backend {
	case BackendS3:
		if c.S3.Bucket == "" {
			return errors.New("S3_BUCKET required for s3 backend")
		}
	case BackendAzure:
		if c.Azure.Account == "" {
			return errors.New("AZURE_STORAGE_ACCOUNT required for azure backend")
		}
		if c.Azure.Key == "" && c.Azure.SASToken == "" {
			return errors.New("AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN required for azure backend")
		}
		if c.Azure.Container == "" {
			return errors.New("AZURE_STORAGE_CONTAINER required for azure backend")
		}
	case BackendGCS:
		if c.GCS.Bucket == "" {
			return errors.New("GCS_BUCKET required for gcs backend")
		}
		// No credentials check — Application Default Credentials resolves
		// itself: GOOGLE_APPLICATION_CREDENTIALS file, gcloud auth, GKE
		// Workload Identity, GCE metadata server, etc.
	case "":
		return errors.New("STORAGE_BACKEND is required")
	default:
		return fmt.Errorf("unknown STORAGE_BACKEND: %q", c.Backend)
	}
	return nil
}
