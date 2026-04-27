/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
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

	// ServiceAccountName runs the CronJob pod under this KSA — required for
	// IRSA (EKS) or Workload Identity (GKE).
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Suspend pauses the CronJob without deleting the resource.
	Suspend bool `json:"suspend,omitempty"`

	// FailedJobsHistoryLimit / SuccessfulJobsHistoryLimit pass through to the
	// generated CronJob (defaults: 7 / 3).
	FailedJobsHistoryLimit     *int32 `json:"failedJobsHistoryLimit,omitempty"`
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`
}

// DatabaseSpec describes the dump source.
type DatabaseSpec struct {
	// Type — one of the 13 supported engines.
	// +kubebuilder:validation:Enum=postgresql;mysql;mariadb;mongodb;cockroach;redis;sqlserver;oracle;elasticsearch;etcd;clickhouse;neo4j;sqlite
	Type string `json:"type"`

	// Host (or path, for sqlite). Optional for sqlite (Name holds the path).
	Host string `json:"host,omitempty"`

	// Port — overrides the engine default.
	Port int32 `json:"port,omitempty"`

	// Name — DB name / index / `db.table` form (clickhouse).
	Name string `json:"name,omitempty"`

	// CredentialsSecretRef — Secret with username/password keys. Optional for
	// redis, etcd, elasticsearch and sqlite.
	// Convention: every field ending in *SecretRef points at a Secret.
	CredentialsSecretRef *DBCredentialsSecretRef `json:"credentialsSecretRef,omitempty"`

	// Options — raw extra flags forwarded to the engine CLI. Use only for
	// non-sensitive flags. Anything carrying a token (e.g. ES Bearer / ApiKey)
	// MUST go through `optionsSecretRef` so it never lands in plaintext on
	// `kubectl describe pod`.
	Options string `json:"options,omitempty"`

	// OptionsSecretRef — when set, DUMP_OPTIONS is fetched from this Secret key.
	// Takes precedence over `options` when both are provided.
	OptionsSecretRef *SecretKeyRef `json:"optionsSecretRef,omitempty"`
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
	Account        string               `json:"account"`
	Container      string               `json:"container"`
	Prefix         string               `json:"prefix,omitempty"`
	Endpoint       string               `json:"endpoint,omitempty"`
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
	Channel    string       `json:"channel,omitempty"`
	Username   string       `json:"username,omitempty"`
}

// DiscordNotifier maps to DISCORD_WEBHOOK_URL.
type DiscordNotifier struct {
	WebhookSecretRef SecretKeyRef `json:"webhookSecretRef"`
	Username   string       `json:"username,omitempty"`
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

	// CurrentRun — name of the BackupRun that is in progress, "" when idle.
	CurrentRun string `json:"currentRun,omitempty"`

	// Conditions follow the standard k8s metav1.Condition contract.
	// Common types: Ready, Healthy.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.database.type`
// +kubebuilder:printcolumn:name="Backend",type=string,JSONPath=`.spec.storage.backend`
// +kubebuilder:printcolumn:name="Last-Success",type=date,JSONPath=`.status.lastSuccessTime`
// +kubebuilder:printcolumn:name="Last-Failure",type=date,JSONPath=`.status.lastFailureTime`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`

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
