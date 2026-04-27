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

	// ServiceAccountName — same as BackupSchedule.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// TTLSecondsAfterFinished — how long to keep the Job after success.
	// Default 24h.
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
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

	// Message — short human-readable explanation, populated on failure.
	Message string `json:"message,omitempty"`

	// Conditions follow the standard k8s metav1.Condition contract.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.database.type`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceKey`
// +kubebuilder:printcolumn:name="Started",type=date,JSONPath=`.status.startedAt`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`

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
