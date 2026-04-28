/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RestoreSpec is the desired state of a one-shot restore. The controller
// creates a Job from the spec; once it completes successfully the Restore
// resource transitions to phase=Succeeded and the Job is left around for
// inspection (subject to TTL).
type RestoreSpec struct {
	// SourceKey — full object key inside the storage bucket / container.
	// Example: pg/daily/YYYY/MM/DD/dump_YYYYMMDD_HHMMSS.sql.gz
	// +kubebuilder:validation:MinLength=1
	SourceKey string `json:"sourceKey"`

	// Database — same shape as BackupSchedule.spec.database. Drives where
	// the dump is applied.
	Database DatabaseSpec `json:"database"`

	// Storage — where the artefact lives (S3 / Azure / GCS).
	Storage StorageSpec `json:"storage"`

	// CreateDB — when true, the restorer issues CREATE DATABASE first
	// (Postgres / MySQL / MariaDB).
	CreateDB bool `json:"createDB,omitempty"`

	// Notifications — same as BackupSchedule.spec.notifications.
	Notifications *NotificationsSpec `json:"notifications,omitempty"`

	// Image overrides the default dumpscript image.
	Image string `json:"image,omitempty"`

	// ImagePullPolicy — passed through to the dumpscript container.
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets — passed through to the Pod spec.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ServiceAccountName — same as BackupSchedule.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// TTLSecondsAfterFinished — how long to keep the Job after success.
	// Default 24h.
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// BackoffLimit — number of retries the Job allows. Defaults to 0
	// (single attempt). Restores rarely benefit from retry.
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`

	// ActiveDeadlineSeconds — kill the Pod after this many seconds even if
	// it's still running. 0 / unset means no deadline.
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// Resources — passed through to the dumpscript container.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector / Tolerations / Affinity / PriorityClassName — standard Pod
	// scheduling overrides.
	NodeSelector      map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations       []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity          *corev1.Affinity    `json:"affinity,omitempty"`
	PriorityClassName string              `json:"priorityClassName,omitempty"`

	// ExtraEnv — additional env vars merged into the dumpscript container.
	// Operator-managed env vars always win on key collision.
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// DryRun — when true, the dumpscript binary validates configuration and
	// destination reachability but skips the actual restore.
	// Maps to env DRY_RUN=true.
	DryRun bool `json:"dryRun,omitempty"`

	// Compression — codec the source artifact is encoded with. The binary
	// auto-detects from the file extension (.gz vs .zst), so this is
	// informational and only used when COMPRESSION_TYPE needs to be set
	// explicitly (e.g. for the post-restore re-compress path).
	// +kubebuilder:validation:Enum=gzip;zstd
	Compression string `json:"compression,omitempty"`

	// RestoreTimeout caps how long the restore command may run.
	// 0/empty falls back to the binary default (2h). Maps to env RESTORE_TIMEOUT.
	RestoreTimeout *metav1.Duration `json:"restoreTimeout,omitempty"`

	// VerifyContent toggles the post-restore verifier (TCP reachability of
	// the configured DB endpoint). Defaults to true on the binary.
	// Maps to env VERIFY_CONTENT.
	VerifyContent *bool `json:"verifyContent,omitempty"`

	// WorkDir — directory inside the pod where the binary writes the
	// downloaded dump before applying it. Defaults to /dumpscript.
	// Maps to env WORK_DIR.
	WorkDir string `json:"workDir,omitempty"`

	// LogLevel — verbosity of the binary's structured logs.
	// +kubebuilder:validation:Enum=debug;info;warn;error
	LogLevel string `json:"logLevel,omitempty"`

	// LogFormat — json (default) or console.
	// +kubebuilder:validation:Enum=json;console
	LogFormat string `json:"logFormat,omitempty"`

	// MetricsListen — when set (e.g. ":9090"), the binary spawns an HTTP
	// listener on that address serving promhttp.Handler() at /metrics.
	// Maps to env METRICS_LISTEN.
	MetricsListen string `json:"metricsListen,omitempty"`

	// Prometheus configures Pushgateway-based metrics export.
	Prometheus *PrometheusSpec `json:"prometheus,omitempty"`
}

// RestorePhase enumerates the lifecycle states of a Restore resource.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type RestorePhase string

const (
	RestorePhasePending   RestorePhase = "Pending"
	RestorePhaseRunning   RestorePhase = "Running"
	RestorePhaseSucceeded RestorePhase = "Succeeded"
	RestorePhaseFailed    RestorePhase = "Failed"
)

// RestoreStatus reflects the most recent state observed.
type RestoreStatus struct {
	// Phase — Pending / Running / Succeeded / Failed.
	Phase RestorePhase `json:"phase,omitempty"`

	// JobName — name of the underlying batch/v1 Job created by the controller.
	JobName string `json:"jobName,omitempty"`

	// StartedAt — when the Job started.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt — when the Job terminated (success or failure).
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// DurationSeconds — duration in seconds from StartedAt to CompletedAt.
	// 0 while the Job is still running.
	DurationSeconds int64 `json:"durationSeconds,omitempty"`

	// Message — short human-readable explanation, populated on failure or
	// success.
	Message string `json:"message,omitempty"`

	// ObservedGeneration — generation observed by the most recent reconcile.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions follow the standard k8s metav1.Condition contract.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.database.type`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceKey`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Started",type=date,JSONPath=`.status.startedAt`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Job",type=string,priority=1,JSONPath=`.status.jobName`
// +kubebuilder:printcolumn:name="Duration",type=integer,priority=1,JSONPath=`.status.durationSeconds`
// +kubebuilder:printcolumn:name="Backend",type=string,priority=1,JSONPath=`.spec.storage.backend`
// +kubebuilder:printcolumn:name="Reason",type=string,priority=1,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`

// Restore is the Schema for the restores API.
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreSpec   `json:"spec,omitempty"`
	Status RestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreList contains a list of Restore.
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Restore{}, &RestoreList{})
}
