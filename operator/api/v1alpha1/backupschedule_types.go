/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupScheduleSpec is the desired state of a recurring dumpscript backup.
type BackupScheduleSpec struct {
	// Schedule is the cron expression that drives the underlying CronJob.
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`

	// Periodicity controls the storage prefix layout and the retention sweep window.
	// +kubebuilder:validation:Enum=daily;weekly;monthly;yearly
	Periodicity string `json:"periodicity"`

	// RetentionDays — backups older than this are deleted before each run.
	// 0 disables retention.
	// +kubebuilder:validation:Minimum=0
	RetentionDays int32 `json:"retentionDays,omitempty"`

	// Database describes which DB to dump and how to authenticate.
	Database DatabaseSpec `json:"database"`

	// Storage selects the destination backend (S3, Azure, or GCS).
	Storage StorageSpec `json:"storage"`

	// Notifications wires Slack / Discord / Teams / Webhook / Stdout.
	Notifications *NotificationsSpec `json:"notifications,omitempty"`

	// Image overrides the default dumpscript image.
	Image string `json:"image,omitempty"`

	// ImagePullPolicy — passed through to the dumpscript container.
	// Defaults to Kubernetes' usual rule (Always for `:latest`, IfNotPresent otherwise).
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets — passed through to the Pod spec for pulling from
	// private registries.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ServiceAccountName runs the CronJob pod under this KSA — required for
	// IRSA (EKS) or Workload Identity (GKE).
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Suspend pauses the CronJob without deleting the resource.
	Suspend bool `json:"suspend,omitempty"`

	// FailedJobsHistoryLimit / SuccessfulJobsHistoryLimit pass through to the
	// generated CronJob (defaults: 7 / 3).
	FailedJobsHistoryLimit     *int32 `json:"failedJobsHistoryLimit,omitempty"`
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// DryRun — when true, the dumpscript binary validates configuration and
	// destination reachability but skips the actual database dump and
	// upload. Useful for validating a freshly applied BackupSchedule.
	// Maps to env DRY_RUN=true.
	DryRun bool `json:"dryRun,omitempty"`

	// Compression — codec used for the on-disk dump artifact.
	// gzip (default) is universally supported; zstd produces ~30% smaller
	// dumps at ~2x the throughput on modern CPUs but requires zstd-aware
	// consumers. Maps to env COMPRESSION_TYPE.
	// MongoDB and etcd always emit gzip regardless of this setting because
	// their dump tools wrap the archive in gzip natively.
	// +kubebuilder:validation:Enum=gzip;zstd
	Compression string `json:"compression,omitempty"`

	// DumpTimeout caps how long a single dump invocation may run.
	// 0/empty falls back to the binary default (2h). Maps to env DUMP_TIMEOUT.
	DumpTimeout *metav1.Duration `json:"dumpTimeout,omitempty"`

	// LockGracePeriod — when a stale lock from a previous run is older than
	// this, the next run takes over instead of skipping. 0/empty disables
	// stale-lock recovery (strict mode). Maps to env LOCK_GRACE_PERIOD.
	LockGracePeriod *metav1.Duration `json:"lockGracePeriod,omitempty"`

	// DumpRetry controls how the binary retries a failed dump command.
	// Useful when the source DB is mid-failover or the network is flaky.
	DumpRetry *RetryPolicy `json:"dumpRetry,omitempty"`

	// VerifyContent toggles the per-engine content verifier (e.g. checking
	// that pg_dump output ends with "-- PostgreSQL database dump complete").
	// Defaults to true on the binary. Maps to env VERIFY_CONTENT.
	VerifyContent *bool `json:"verifyContent,omitempty"`

	// WorkDir — directory inside the pod where the binary writes the
	// temporary dump file before uploading. Defaults to /dumpscript.
	// Maps to env WORK_DIR.
	WorkDir string `json:"workDir,omitempty"`

	// LogLevel — verbosity of the binary's structured logs.
	// +kubebuilder:validation:Enum=debug;info;warn;error
	LogLevel string `json:"logLevel,omitempty"`

	// LogFormat — json (default, ingestible by log aggregators) or console
	// (ANSI colors + human-readable durations, useful for kubectl logs).
	// +kubebuilder:validation:Enum=json;console
	LogFormat string `json:"logFormat,omitempty"`

	// MetricsListen — when set (e.g. ":9090"), the binary spawns an HTTP
	// listener on that address serving promhttp.Handler() at /metrics.
	// CronJob-style runs typically leave this empty and rely on the
	// Pushgateway path (see Prometheus) or the operator's own metrics
	// endpoint. Maps to env METRICS_LISTEN.
	MetricsListen string `json:"metricsListen,omitempty"`

	// Prometheus configures Pushgateway-based metrics export.
	Prometheus *PrometheusSpec `json:"prometheus,omitempty"`

	// ConcurrencyPolicy — what to do when the next run is due while the
	// previous Job is still active. Defaults to Forbid (skip the new run).
	// Allow runs in parallel; Replace cancels the old run.
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	ConcurrencyPolicy batchv1.ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// StartingDeadlineSeconds — the CronJob skips the run if the controller
	// missed its window by more than this many seconds. Useful when a cluster
	// outage delays scheduling and you'd rather skip than catch up.
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`

	// BackoffLimit — number of retries the produced Job allows before marking
	// itself Failed. Defaults to 0 (single attempt) — backups should fail
	// loud, not silently retry.
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`

	// ActiveDeadlineSeconds — kill the Pod after this many seconds even if
	// it's still running. 0 / unset means no deadline.
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// Resources — passed through to the dumpscript container. Useful for
	// large databases that need more memory or CPU than the default.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector, Tolerations, Affinity, PriorityClassName — standard Pod
	// scheduling overrides, useful for "run backups on dedicated nodes" or
	// "preempt low-priority workloads".
	NodeSelector      map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations       []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity          *corev1.Affinity    `json:"affinity,omitempty"`
	PriorityClassName string              `json:"priorityClassName,omitempty"`

	// ExtraEnv — additional env vars merged into the dumpscript container
	// (e.g., HTTPS_PROXY, NO_PROXY, custom locale). Operator-managed env vars
	// always win on key collision.
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
}

// RetryPolicy describes how a failed operation should be retried.
type RetryPolicy struct {
	// MaxAttempts — total attempts including the first one. Set to 1 to
	// disable retry while still going through the retry decorator.
	// +kubebuilder:validation:Minimum=1
	MaxAttempts int32 `json:"maxAttempts,omitempty"`

	// InitialBackoff — delay before the first retry. Subsequent retries
	// double up to MaxBackoff.
	InitialBackoff *metav1.Duration `json:"initialBackoff,omitempty"`

	// MaxBackoff caps the exponential backoff between retries.
	MaxBackoff *metav1.Duration `json:"maxBackoff,omitempty"`
}

// PrometheusSpec configures the binary's Prometheus Pushgateway metrics.
// Use MetricsListen on the parent spec for pull-based scraping instead.
type PrometheusSpec struct {
	// Enabled — must be true for any Prometheus env to be wired.
	Enabled bool `json:"enabled,omitempty"`

	// PushgatewayURL — full URL to a Pushgateway instance.
	PushgatewayURL string `json:"pushgatewayURL,omitempty"`

	// JobName — Pushgateway job label. Defaults to "dumpscript".
	JobName string `json:"jobName,omitempty"`

	// Instance — Pushgateway instance label. Empty leaves it unset (the
	// Pushgateway will use the source IP).
	Instance string `json:"instance,omitempty"`

	// LogOnExit — when true, the binary also prints the metrics in OpenMetrics
	// format on stderr at process exit. Useful for log-based dashboards.
	LogOnExit bool `json:"logOnExit,omitempty"`
}

// DatabaseSpec describes the dump source.
type DatabaseSpec struct {
	// Type — one of the 13 supported engines.
	// +kubebuilder:validation:Enum=postgresql;mysql;mariadb;mongodb;cockroach;redis;sqlserver;oracle;elasticsearch;etcd;clickhouse;neo4j;sqlite
	Type string `json:"type"`

	// Host (or path, for sqlite). Optional for sqlite (Name holds the path).
	Host string `json:"host,omitempty"`

	// Port — overrides the engine default. Engine defaults are applied by the
	// operator when this field is left at 0 (postgres=5432, mysql/mariadb=3306,
	// mongodb=27017, redis=6379, etcd=2379, sqlserver=1433, oracle=1521,
	// elasticsearch=9200, clickhouse=9000, neo4j=7687, cockroach=26257).
	Port int32 `json:"port,omitempty"`

	// Name — DB name / index / `db.table` form (clickhouse) / file path (sqlite).
	Name string `json:"name,omitempty"`

	// CredentialsSecretRef — Secret with username/password keys. Optional for
	// redis, etcd, elasticsearch and sqlite.
	// Convention: every field ending in *SecretRef points at a Secret.
	CredentialsSecretRef *DBCredentialsSecretRef `json:"credentialsSecretRef,omitempty"`

	// MongoDB — MongoDB-specific options. Only consulted when type=mongodb.
	MongoDB *MongoDBSpec `json:"mongodb,omitempty"`

	// PostgreSQL — Postgres-specific options. Only consulted when type=postgresql.
	PostgreSQL *PostgreSQLSpec `json:"postgresql,omitempty"`

	// MySQL — MySQL-specific options. Only consulted when type=mysql.
	MySQL *MySQLSpec `json:"mysql,omitempty"`

	// MariaDB — MariaDB-specific options. Only consulted when type=mariadb.
	MariaDB *MariaDBSpec `json:"mariadb,omitempty"`

	// Redis — Redis-specific options. Only consulted when type=redis.
	Redis *RedisSpec `json:"redis,omitempty"`

	// Etcd — etcd-specific options. Only consulted when type=etcd.
	Etcd *EtcdSpec `json:"etcd,omitempty"`

	// Elasticsearch — Elasticsearch-specific options. Only consulted when type=elasticsearch.
	Elasticsearch *ElasticsearchSpec `json:"elasticsearch,omitempty"`

	// SQLServer — SQL Server-specific options. Only consulted when type=sqlserver.
	SQLServer *SQLServerSpec `json:"sqlserver,omitempty"`

	// Oracle — Oracle-specific options. Only consulted when type=oracle.
	Oracle *OracleSpec `json:"oracle,omitempty"`

	// ClickHouse — ClickHouse-specific options. Only consulted when type=clickhouse.
	ClickHouse *ClickHouseSpec `json:"clickhouse,omitempty"`

	// Neo4j — Neo4j-specific options. Only consulted when type=neo4j.
	Neo4j *Neo4jSpec `json:"neo4j,omitempty"`

	// Cockroach — CockroachDB-specific options. Only consulted when type=cockroach.
	Cockroach *CockroachSpec `json:"cockroach,omitempty"`

	// Volume — when set, the operator mounts a Volume into the dumpscript pod.
	// Required for sqlite (and any other file-based engine) so the dumpscript
	// container can read/write the database file.
	Volume *DatabaseVolume `json:"volume,omitempty"`

	// Options — raw extra flags forwarded to the engine CLI. Use only for
	// non-sensitive flags. Anything carrying a token (e.g. ES Bearer / ApiKey)
	// MUST go through `optionsSecretRef` so it never lands in plaintext on
	// `kubectl describe pod`.
	Options string `json:"options,omitempty"`

	// OptionsSecretRef — when set, DUMP_OPTIONS is fetched from this Secret key.
	// Takes precedence over `options` when both are provided.
	OptionsSecretRef *SecretKeyRef `json:"optionsSecretRef,omitempty"`
}

// MongoDBSpec carries MongoDB-only options exposed as type-safe fields so users
// don't have to encode them in the opaque `options` string.
type MongoDBSpec struct {
	// AuthSource — MongoDB authentication database (typically "admin" when the
	// user lives in the admin DB). Translated by the operator into
	// `--authenticationDatabase=<value>` appended to DUMP_OPTIONS so it flows
	// through to both mongodump and mongorestore.
	// +kubebuilder:validation:MinLength=1
	AuthSource string `json:"authSource,omitempty"`
}

// PostgreSQLSpec carries Postgres-only options.
type PostgreSQLSpec struct {
	// Version — server major version (e.g. "16", "17"). The dumpscript image
	// ships pg_dump 18 which dumps all PG 9.2+ servers, so this is informational
	// today; the binary surfaces it as POSTGRES_VERSION env so future engine
	// logic / observability can pivot on it. Defaults to "16" on the binary.
	Version string `json:"version,omitempty"`
}

// MySQLSpec carries MySQL-only options.
type MySQLSpec struct {
	// Version — server major version (e.g. "8.0"). Maps to MYSQL_VERSION env.
	Version string `json:"version,omitempty"`
}

// MariaDBSpec carries MariaDB-only options.
type MariaDBSpec struct {
	// Version — server major version (e.g. "11.4"). Maps to MARIADB_VERSION env.
	Version string `json:"version,omitempty"`
}

// RedisSpec carries Redis-only options. All fields below are translated by
// the operator into raw flags appended to DUMP_OPTIONS.
type RedisSpec struct {
	// DB — numeric Redis logical database (0-15 by default). Translated to
	// `-n <value>` on the redis-cli argv.
	// +kubebuilder:validation:Minimum=0
	DB int32 `json:"db,omitempty"`

	// TLS — when true, the dumper passes `--tls` to redis-cli. The operator
	// does not yet wire CA/cert/key Secret refs; users that need them can
	// pass extra flags via `database.options`.
	TLS bool `json:"tls,omitempty"`
}

// EtcdSpec carries etcd-only options.
type EtcdSpec struct {
	// Scheme — `http` (default) or `https`. The dumper consumes
	// `--scheme=https` from DUMP_OPTIONS to pick the URL scheme without
	// passing it to etcdctl. This field is the type-safe equivalent.
	// +kubebuilder:validation:Enum=http;https
	Scheme string `json:"scheme,omitempty"`
}

// ElasticsearchSpec carries Elasticsearch-only options.
//
// Bearer/ApiKey auth tokens go through `database.optionsSecretRef` instead
// of a typed field — they're naturally raw `--auth-header=…` flags that the
// existing optionsSecretRef path already handles without exposing the token
// in plain CR YAML.
type ElasticsearchSpec struct {
	// IndexPattern — value for `--index-pattern`. When empty, the dumper
	// dumps the index named by `database.name`.
	IndexPattern string `json:"indexPattern,omitempty"`
}

// SQLServerSpec carries SQL Server-only options.
type SQLServerSpec struct {
	// TrustServerCertificate — passes `-W` (trust-server-cert) to
	// mssql-scripter. Useful for self-signed certs in dev/test.
	TrustServerCertificate bool `json:"trustServerCertificate,omitempty"`

	// ApplicationIntent — `ReadOnly` to read from a secondary replica.
	// +kubebuilder:validation:Enum=ReadOnly;ReadWrite
	ApplicationIntent string `json:"applicationIntent,omitempty"`
}

// OracleSpec carries Oracle-only options.
type OracleSpec struct {
	// ServiceName — Oracle service name. When set, the dumper builds the
	// connection string using a service descriptor instead of the SID
	// (which is what `database.name` is interpreted as by default).
	ServiceName string `json:"serviceName,omitempty"`
}

// ClickHouseSpec carries ClickHouse-only options.
type ClickHouseSpec struct {
	// Cluster — ON CLUSTER target. Translates to `--cluster=<name>`.
	Cluster string `json:"cluster,omitempty"`

	// Secure — when true, passes `--secure` to clickhouse-client (TLS).
	Secure bool `json:"secure,omitempty"`
}

// Neo4jSpec carries Neo4j-only options.
type Neo4jSpec struct {
	// AuthMode — `bolt` (default) or `none`. Affects how neo4j-admin
	// authenticates to the running server.
	// +kubebuilder:validation:Enum=bolt;none
	AuthMode string `json:"authMode,omitempty"`
}

// CockroachSpec carries CockroachDB-only options.
type CockroachSpec struct {
	// SSLMode — `disable`, `require`, `verify-ca`, `verify-full`. Maps to
	// `sslmode=<value>` in the connection string the dumper builds for psql.
	// +kubebuilder:validation:Enum=disable;require;verify-ca;verify-full
	SSLMode string `json:"sslMode,omitempty"`
}

// DatabaseVolume mounts a single Volume into the dumpscript Pod at MountPath.
// Exactly one of the volume sources should be set; if multiple are set, the
// operator picks the first non-nil in the order PVC → EmptyDir → ConfigMap →
// Secret.
//
// +kubebuilder:validation:XValidation:rule="has(self.persistentVolumeClaim) || has(self.emptyDir) || has(self.configMap) || has(self.secret)",message="at least one volume source must be set"
type DatabaseVolume struct {
	// MountPath — where the volume is mounted inside the dumpscript container.
	// +kubebuilder:validation:MinLength=1
	MountPath string `json:"mountPath"`

	// PersistentVolumeClaim — typical choice for stateful file-based DBs.
	PersistentVolumeClaim *corev1.PersistentVolumeClaimVolumeSource `json:"persistentVolumeClaim,omitempty"`

	// EmptyDir — useful when an initContainer (out of operator scope) seeds
	// the file before dumpscript runs.
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`

	// ConfigMap — mount a sqlite file shipped via ConfigMap (for tiny fixtures).
	ConfigMap *corev1.ConfigMapVolumeSource `json:"configMap,omitempty"`

	// Secret — mount a sqlite file (or other DB file) from a Secret.
	Secret *corev1.SecretVolumeSource `json:"secret,omitempty"`
}

// DBCredentialsSecretRef references a Secret with the database credentials.
type DBCredentialsSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// UsernameKey defaults to "username" when empty.
	UsernameKey string `json:"usernameKey,omitempty"`
	// PasswordKey defaults to "password" when empty.
	PasswordKey string `json:"passwordKey,omitempty"`
}

// StorageSpec selects the destination backend.
//
// +kubebuilder:validation:XValidation:rule="self.backend != 's3' || has(self.s3)",message="storage.s3 is required when backend is s3"
// +kubebuilder:validation:XValidation:rule="self.backend != 'azure' || has(self.azure)",message="storage.azure is required when backend is azure"
// +kubebuilder:validation:XValidation:rule="self.backend != 'gcs' || has(self.gcs)",message="storage.gcs is required when backend is gcs"
type StorageSpec struct {
	// Backend — s3, azure, or gcs.
	// +kubebuilder:validation:Enum=s3;azure;gcs
	Backend string `json:"backend"`

	S3    *S3Storage    `json:"s3,omitempty"`
	Azure *AzureStorage `json:"azure,omitempty"`
	GCS   *GCSStorage   `json:"gcs,omitempty"`

	// UploadCutoff / ChunkSize / Concurrency override the dumpscript upload
	// tuning defaults (200M / 100M / 4).
	UploadCutoff string `json:"uploadCutoff,omitempty"`
	ChunkSize    string `json:"chunkSize,omitempty"`
	Concurrency  int32  `json:"concurrency,omitempty"`
}

// S3Storage configures any S3-compatible backend (AWS, MinIO, GCS via HMAC,
// Wasabi, B2 via endpoint override, etc.).
type S3Storage struct {
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix,omitempty"`
	Region       string `json:"region,omitempty"`
	EndpointURL  string `json:"endpointURL,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	// RoleARN — IRSA assume-role ARN. When set, the controller wires
	// AWS_ROLE_ARN and skips static keys.
	RoleARN string `json:"roleARN,omitempty"`
	// CredentialsSecretRef — Secret with AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
	// (and optional AWS_SESSION_TOKEN). Omit when using IRSA.
	CredentialsSecretRef *S3CredentialsSecretRef `json:"credentialsSecretRef,omitempty"`

	// SSE — server-side encryption algorithm. Valid values: "AES256" (S3-
	// managed keys) or "aws:kms" (KMS encryption with the optional
	// SSEKMSKeyID below — empty falls back to the bucket's default KMS key).
	// Empty disables server-side encryption beyond bucket defaults.
	// Maps to env S3_SSE.
	// +kubebuilder:validation:Pattern=`^(AES256|aws:kms)$`
	SSE string `json:"sse,omitempty"`

	// SSEKMSKeyID — KMS key ARN/alias used when SSE=aws:kms.
	// Maps to env S3_SSE_KMS_KEY_ID.
	SSEKMSKeyID string `json:"sseKMSKeyID,omitempty"`
}

// S3CredentialsSecretRef points at static AWS credentials in a Secret.
type S3CredentialsSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// AccessKeyIDKey defaults to "AWS_ACCESS_KEY_ID".
	AccessKeyIDKey string `json:"accessKeyIDKey,omitempty"`
	// SecretAccessKeyKey defaults to "AWS_SECRET_ACCESS_KEY".
	SecretAccessKeyKey string `json:"secretAccessKeyKey,omitempty"`
	// SessionTokenKey is optional; only used when present in the Secret.
	SessionTokenKey string `json:"sessionTokenKey,omitempty"`
}

// AzureStorage configures Azure Blob.
type AzureStorage struct {
	Account              string                     `json:"account"`
	Container            string                     `json:"container"`
	Prefix               string                     `json:"prefix,omitempty"`
	Endpoint             string                     `json:"endpoint,omitempty"`
	CredentialsSecretRef *AzureCredentialsSecretRef `json:"credentialsSecretRef,omitempty"`
}

// AzureCredentialsSecretRef holds either a Shared Key or a SAS token in a Secret.
type AzureCredentialsSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// SharedKeyKey is the Secret key holding the Storage Account Shared Key.
	SharedKeyKey string `json:"sharedKeyKey,omitempty"`
	// SASTokenKey is the Secret key holding a SAS token (alternative to SharedKey).
	SASTokenKey string `json:"sasTokenKey,omitempty"`
}

// GCSStorage configures the native GCS backend (uses Application Default
// Credentials — typically Workload Identity on GKE).
type GCSStorage struct {
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix,omitempty"`
	ProjectID string `json:"projectID,omitempty"`
	// Endpoint — optional override for the GCS API URL. Used by the
	// fake-gcs-server emulator for tests and for self-hosted GCS-compatible
	// services. When set, authentication is disabled (the emulator accepts
	// unauthenticated traffic). Leave empty in production.
	Endpoint string `json:"endpoint,omitempty"`
	// CredentialsSecretRef — optional override; mounts a Service Account JSON key
	// as a read-only volume. Leave empty in GKE to use Workload Identity.
	CredentialsSecretRef *GCSCredentialsSecretRef `json:"credentialsSecretRef,omitempty"`
}

// GCSCredentialsSecretRef points at a Secret with a service-account JSON key.
type GCSCredentialsSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// KeyFile — Secret key holding the JSON file (defaults to "key.json").
	KeyFile string `json:"keyFile,omitempty"`
}

// NotificationsSpec wires every supported notifier. Each one is independent;
// you can configure 1, 2, or all at once.
type NotificationsSpec struct {
	NotifySuccess bool             `json:"notifySuccess,omitempty"`
	Slack         *SlackNotifier   `json:"slack,omitempty"`
	Discord       *DiscordNotifier `json:"discord,omitempty"`
	Teams         *TeamsNotifier   `json:"teams,omitempty"`
	Webhook       *WebhookNotifier `json:"webhook,omitempty"`
	Stdout        bool             `json:"stdout,omitempty"`
}

// SlackNotifier maps to SLACK_WEBHOOK_URL + SLACK_CHANNEL/USERNAME.
type SlackNotifier struct {
	WebhookSecretRef SecretKeyRef `json:"webhookSecretRef"`
	Channel          string       `json:"channel,omitempty"`
	Username         string       `json:"username,omitempty"`
}

// DiscordNotifier maps to DISCORD_WEBHOOK_URL.
type DiscordNotifier struct {
	WebhookSecretRef SecretKeyRef `json:"webhookSecretRef"`
	Username         string       `json:"username,omitempty"`
}

// TeamsNotifier maps to TEAMS_WEBHOOK_URL.
type TeamsNotifier struct {
	WebhookSecretRef SecretKeyRef `json:"webhookSecretRef"`
}

// WebhookNotifier — generic JSON POST.
type WebhookNotifier struct {
	URLSecretRef        SecretKeyRef  `json:"urlSecretRef"`
	AuthHeaderSecretRef *SecretKeyRef `json:"authHeaderSecretRef,omitempty"`
}

// SecretKeyRef points at a single key inside a Secret.
type SecretKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// BackupScheduleStatus reflects the most recent state observed.
type BackupScheduleStatus struct {
	// LastScheduleTime — wall-clock when the controller last triggered a run.
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessTime — wall-clock of the most recent successful run.
	LastSuccessTime *metav1.Time `json:"lastSuccessTime,omitempty"`

	// LastFailureTime — wall-clock of the most recent failed run.
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`

	// LastRetentionTime — wall-clock of the most recent run that had a
	// non-zero RetentionDays (the dumpscript container runs the prune as part
	// of that Job). Best-effort: this only records that the prune *was
	// triggered*, not whether it succeeded — the dumpscript container's logs
	// are authoritative.
	LastRetentionTime *metav1.Time `json:"lastRetentionTime,omitempty"`

	// LastJobName — name of the most recent terminated Job (success or
	// failure), useful for `kubectl logs jobs/<name>` without listing.
	LastJobName string `json:"lastJobName,omitempty"`

	// LastDurationSeconds — duration in seconds of the most recent terminated
	// Job (success or failure). 0 when no Job has completed yet.
	LastDurationSeconds int64 `json:"lastDurationSeconds,omitempty"`

	// TotalRuns — total number of terminated Jobs the operator has observed
	// for this BackupSchedule (success + failure). Approximate: limited by
	// SuccessfulJobsHistoryLimit / FailedJobsHistoryLimit retention.
	TotalRuns int64 `json:"totalRuns,omitempty"`

	// ConsecutiveFailures — number of consecutive failed Jobs since the last
	// success. Resets to 0 on the next successful run. Useful for alerting
	// on "broken backup" without parsing logs.
	ConsecutiveFailures int32 `json:"consecutiveFailures,omitempty"`

	// CurrentRun — name of the BackupRun that is in progress, "" when idle.
	CurrentRun string `json:"currentRun,omitempty"`

	// ObservedGeneration — generation observed by the most recent reconcile.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions follow the standard k8s metav1.Condition contract.
	// Common types: Ready, Healthy.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.database.type`
// +kubebuilder:printcolumn:name="Backend",type=string,JSONPath=`.spec.storage.backend`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Last-Success",type=date,JSONPath=`.status.lastSuccessTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Periodicity",type=string,priority=1,JSONPath=`.spec.periodicity`
// +kubebuilder:printcolumn:name="Retention",type=integer,priority=1,JSONPath=`.spec.retentionDays`
// +kubebuilder:printcolumn:name="Last-Failure",type=date,priority=1,JSONPath=`.status.lastFailureTime`
// +kubebuilder:printcolumn:name="Current-Run",type=string,priority=1,JSONPath=`.status.currentRun`
// +kubebuilder:printcolumn:name="Last-Job",type=string,priority=1,JSONPath=`.status.lastJobName`
// +kubebuilder:printcolumn:name="Last-Duration",type=integer,priority=1,JSONPath=`.status.lastDurationSeconds`
// +kubebuilder:printcolumn:name="Total-Runs",type=integer,priority=1,JSONPath=`.status.totalRuns`
// +kubebuilder:printcolumn:name="Failures",type=integer,priority=1,JSONPath=`.status.consecutiveFailures`
// +kubebuilder:printcolumn:name="Reason",type=string,priority=1,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.conditions[?(@.type=="Ready")].message`

// BackupSchedule is the Schema for the backupschedules API.
type BackupSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupScheduleSpec   `json:"spec,omitempty"`
	Status BackupScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupScheduleList contains a list of BackupSchedule.
type BackupScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupSchedule{}, &BackupScheduleList{})
}
