/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package controller

import (
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dumpscriptv1alpha1 "github.com/cloudscript-technology/dumpscript/operator/api/v1alpha1"
)

// defaultImage is used when BackupSchedule.spec.image / Restore.spec.image
// is empty. Operators usually override this through helm values or a
// ConfigMap; the constant keeps the controller usable out-of-the-box.
const defaultImage = "ghcr.io/cloudscript-technology/dumpscript:latest"

// buildCronJob materialises a BackupSchedule into a batch/v1 CronJob.
// The reconciler then create-or-updates the resulting object.
func buildCronJob(bs *dumpscriptv1alpha1.BackupSchedule) *batchv1.CronJob {
	suspend := bs.Spec.Suspend
	successHist := int32Ptr(3)
	if bs.Spec.SuccessfulJobsHistoryLimit != nil {
		successHist = bs.Spec.SuccessfulJobsHistoryLimit
	}
	failHist := int32Ptr(7)
	if bs.Spec.FailedJobsHistoryLimit != nil {
		failHist = bs.Spec.FailedJobsHistoryLimit
	}

	volumes, mounts := secretVolumes(bs.Spec.Storage)
	irsaVol, irsaMount := irsaVolume(bs.Spec.Storage)
	if irsaVol != nil {
		volumes = append(volumes, *irsaVol)
		mounts = append(mounts, *irsaMount)
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bs.Name,
			Namespace: bs.Namespace,
			Labels:    cronLabels(bs.Name),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   bs.Spec.Schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			Suspend:                    &suspend,
			SuccessfulJobsHistoryLimit: successHist,
			FailedJobsHistoryLimit:     failHist,
			JobTemplate: batchv1.JobTemplateSpec{
				// Labels on the Job object (not just the Pod) let refreshStatus
				// find and correlate Jobs back to their BackupSchedule.
				ObjectMeta: metav1.ObjectMeta{Labels: cronLabels(bs.Name)},
				Spec: batchv1.JobSpec{
					BackoffLimit: int32Ptr(0),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: cronLabels(bs.Name)},
						Spec: corev1.PodSpec{
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							ServiceAccountName: bs.Spec.ServiceAccountName,
							Volumes:            volumes,
							Containers: []corev1.Container{{
								Name:         "dumpscript",
								Image:        imageOrDefault(bs.Spec.Image),
								Args:         []string{"dump"},
								Env:          buildEnv(bs.Spec.Database, bs.Spec.Storage, bs.Spec.Notifications, bs.Spec.Periodicity, bs.Spec.RetentionDays),
								VolumeMounts: mounts,
							}},
						},
					},
				},
			},
		},
	}
}

// buildRestoreJob materialises a Restore into a one-shot batch/v1 Job.
func buildRestoreJob(r *dumpscriptv1alpha1.Restore) *batchv1.Job {
	ttl := int32(86400) // 24h default
	if r.Spec.TTLSecondsAfterFinished != nil {
		ttl = *r.Spec.TTLSecondsAfterFinished
	}
	env := buildEnv(r.Spec.Database, r.Spec.Storage, r.Spec.Notifications, "", 0)
	env = append(env, corev1.EnvVar{Name: "S3_KEY", Value: r.Spec.SourceKey})
	if r.Spec.CreateDB {
		env = append(env, corev1.EnvVar{Name: "CREATE_DB", Value: "true"})
	}
	volumes, mounts := secretVolumes(r.Spec.Storage)
	irsaVol, irsaMount := irsaVolume(r.Spec.Storage)
	if irsaVol != nil {
		volumes = append(volumes, *irsaVol)
		mounts = append(mounts, *irsaMount)
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restore-" + r.Name,
			Namespace: r.Namespace,
			Labels:    restoreLabels(r.Name),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(0),
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: restoreLabels(r.Name)},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: r.Spec.ServiceAccountName,
					Volumes:            volumes,
					Containers: []corev1.Container{{
						Name:         "dumpscript",
						Image:        imageOrDefault(r.Spec.Image),
						Args:         []string{"restore"},
						Env:          env,
						VolumeMounts: mounts,
					}},
				},
			},
		},
	}
}

// irsaVolume returns the projected ServiceAccount token Volume and VolumeMount
// required for IRSA (AWS_ROLE_ARN + sts:AssumeRoleWithWebIdentity) when
// spec.storage.s3.roleARN is set. Without this volume the SDK cannot find the
// token file and falls back to static credentials.
//
// On EKS, the pod-identity webhook already injects this volume automatically;
// here we add it ourselves so IRSA works on any OIDC-capable cluster (GKE,
// kind + local OIDC, vanilla Kubernetes with dex, etc.).
func irsaVolume(s dumpscriptv1alpha1.StorageSpec) (*corev1.Volume, *corev1.VolumeMount) {
	if s.Backend != "s3" || s.S3 == nil || s.S3.RoleARN == "" {
		return nil, nil
	}
	ttl := int64(86400)
	vol := &corev1.Volume{
		Name: "aws-iam-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
						Audience:          "sts.amazonaws.com",
						ExpirationSeconds: &ttl,
						Path:              "token",
					},
				}},
			},
		},
	}
	mount := &corev1.VolumeMount{
		Name:      "aws-iam-token",
		MountPath: "/var/run/secrets/eks.amazonaws.com/serviceaccount",
		ReadOnly:  true,
	}
	return vol, mount
}

// secretVolumes returns Volumes + VolumeMounts that materialise sensitive
// files inside the pod (currently only the GCS service-account JSON, which
// must be a real file on disk for `cloud.google.com/go/storage` to read).
//
// Returns nil/nil when there is nothing sensitive to mount — this is the
// happy path for IRSA, Workload Identity, and Shared-Key/SAS auth.
func secretVolumes(s dumpscriptv1alpha1.StorageSpec) ([]corev1.Volume, []corev1.VolumeMount) {
	if s.Backend != "gcs" || s.GCS == nil || s.GCS.CredentialsSecretRef == nil {
		return nil, nil
	}
	c := s.GCS.CredentialsSecretRef
	const volName = "gcs-credentials"
	return []corev1.Volume{{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  c.Name,
					DefaultMode: int32Ptr(0o400),
				},
			},
		}},
		[]corev1.VolumeMount{{
			Name:      volName,
			MountPath: "/var/run/gcs",
			ReadOnly:  true,
		}}
}

// buildEnv translates the typed CR fields into the env-var contract that
// dumpscript itself reads (see internal/config/config.go).
func buildEnv(db dumpscriptv1alpha1.DatabaseSpec, s dumpscriptv1alpha1.StorageSpec, n *dumpscriptv1alpha1.NotificationsSpec, periodicity string, retentionDays int32) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "DB_TYPE", Value: db.Type},
		{Name: "DB_HOST", Value: db.Host},
		{Name: "DB_NAME", Value: db.Name},
	}
	// DUMP_OPTIONS may carry tokens (e.g. Elasticsearch --auth-header=Bearer
	// xxx). Prefer the SecretKeyRef variant when set; fall back to the plain
	// field for non-sensitive flags.
	switch {
	case db.OptionsSecretRef != nil:
		env = append(env, fromSecret("DUMP_OPTIONS", db.OptionsSecretRef.Name, db.OptionsSecretRef.Key))
	case db.Options != "":
		env = append(env, corev1.EnvVar{Name: "DUMP_OPTIONS", Value: db.Options})
	}
	if db.Port != 0 {
		env = append(env, corev1.EnvVar{Name: "DB_PORT", Value: strconv.Itoa(int(db.Port))})
	}
	if db.CredentialsSecretRef != nil {
		env = append(env,
			fromSecret("DB_USER", db.CredentialsSecretRef.Name, keyOr(db.CredentialsSecretRef.UsernameKey, "username")),
			fromSecret("DB_PASSWORD", db.CredentialsSecretRef.Name, keyOr(db.CredentialsSecretRef.PasswordKey, "password")),
		)
	}
	if periodicity != "" {
		env = append(env, corev1.EnvVar{Name: "PERIODICITY", Value: periodicity})
	}
	if retentionDays > 0 {
		env = append(env, corev1.EnvVar{Name: "RETENTION_DAYS", Value: strconv.Itoa(int(retentionDays))})
	}
	env = append(env, storageEnv(s)...)
	env = append(env, notifierEnv(n)...)
	return env
}

func storageEnv(s dumpscriptv1alpha1.StorageSpec) []corev1.EnvVar {
	env := []corev1.EnvVar{{Name: "STORAGE_BACKEND", Value: s.Backend}}
	if s.UploadCutoff != "" {
		env = append(env, corev1.EnvVar{Name: "STORAGE_UPLOAD_CUTOFF", Value: s.UploadCutoff})
	}
	if s.ChunkSize != "" {
		env = append(env, corev1.EnvVar{Name: "STORAGE_CHUNK_SIZE", Value: s.ChunkSize})
	}
	if s.Concurrency > 0 {
		env = append(env, corev1.EnvVar{Name: "STORAGE_UPLOAD_CONCURRENCY", Value: strconv.Itoa(int(s.Concurrency))})
	}

	switch s.Backend {
	case "s3":
		if s.S3 == nil {
			return env
		}
		env = append(env,
			corev1.EnvVar{Name: "S3_BUCKET", Value: s.S3.Bucket},
			corev1.EnvVar{Name: "S3_PREFIX", Value: s.S3.Prefix},
			corev1.EnvVar{Name: "AWS_REGION", Value: s.S3.Region},
		)
		if s.S3.EndpointURL != "" {
			env = append(env, corev1.EnvVar{Name: "AWS_S3_ENDPOINT_URL", Value: s.S3.EndpointURL})
		}
		if s.S3.StorageClass != "" {
			env = append(env, corev1.EnvVar{Name: "S3_STORAGE_CLASS", Value: s.S3.StorageClass})
		}
		if s.S3.RoleARN != "" {
			env = append(env,
				corev1.EnvVar{Name: "AWS_ROLE_ARN", Value: s.S3.RoleARN},
				// Token file path must match the irsaVolume mount path.
				corev1.EnvVar{
					Name:  "AWS_WEB_IDENTITY_TOKEN_FILE",
					Value: "/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
				},
			)
			// When a custom S3 endpoint is configured (e.g. LocalStack, on-prem),
			// redirect STS to the same host so AssumeRoleWithWebIdentity works.
			// On EKS this is not needed — the real STS endpoint is used.
			if s.S3.EndpointURL != "" {
				env = append(env, corev1.EnvVar{
					Name:  "AWS_ENDPOINT_URL_STS",
					Value: s.S3.EndpointURL,
				})
			}
		}
		if c := s.S3.CredentialsSecretRef; c != nil {
			env = append(env,
				fromSecret("AWS_ACCESS_KEY_ID", c.Name, keyOr(c.AccessKeyIDKey, "AWS_ACCESS_KEY_ID")),
				fromSecret("AWS_SECRET_ACCESS_KEY", c.Name, keyOr(c.SecretAccessKeyKey, "AWS_SECRET_ACCESS_KEY")),
			)
			if c.SessionTokenKey != "" {
				env = append(env, fromSecret("AWS_SESSION_TOKEN", c.Name, c.SessionTokenKey))
			}
		}
	case "azure":
		if s.Azure == nil {
			return env
		}
		env = append(env,
			corev1.EnvVar{Name: "AZURE_STORAGE_ACCOUNT", Value: s.Azure.Account},
			corev1.EnvVar{Name: "AZURE_STORAGE_CONTAINER", Value: s.Azure.Container},
			corev1.EnvVar{Name: "AZURE_STORAGE_PREFIX", Value: s.Azure.Prefix},
		)
		if s.Azure.Endpoint != "" {
			env = append(env, corev1.EnvVar{Name: "AZURE_STORAGE_ENDPOINT", Value: s.Azure.Endpoint})
		}
		if c := s.Azure.CredentialsSecretRef; c != nil {
			if c.SharedKeyKey != "" {
				env = append(env, fromSecret("AZURE_STORAGE_KEY", c.Name, c.SharedKeyKey))
			}
			if c.SASTokenKey != "" {
				env = append(env, fromSecret("AZURE_STORAGE_SAS_TOKEN", c.Name, c.SASTokenKey))
			}
		}
	case "gcs":
		if s.GCS == nil {
			return env
		}
		env = append(env,
			corev1.EnvVar{Name: "GCS_BUCKET", Value: s.GCS.Bucket},
			corev1.EnvVar{Name: "GCS_PREFIX", Value: s.GCS.Prefix},
		)
		if s.GCS.ProjectID != "" {
			env = append(env, corev1.EnvVar{Name: "GCS_PROJECT_ID", Value: s.GCS.ProjectID})
		}
		if s.GCS.Endpoint != "" {
			env = append(env, corev1.EnvVar{Name: "GCS_ENDPOINT", Value: s.GCS.Endpoint})
		}
		// Workload Identity path: leave creds empty.
		if c := s.GCS.CredentialsSecretRef; c != nil {
			env = append(env, corev1.EnvVar{
				Name:  "GCS_CREDENTIALS_FILE",
				Value: fmt.Sprintf("/var/run/gcs/%s", keyOr(c.KeyFile, "key.json")),
			})
		}
	}
	return env
}

func notifierEnv(n *dumpscriptv1alpha1.NotificationsSpec) []corev1.EnvVar {
	if n == nil {
		return nil
	}
	var out []corev1.EnvVar
	if n.Slack != nil {
		out = append(out,
			fromSecret("SLACK_WEBHOOK_URL", n.Slack.WebhookSecretRef.Name, n.Slack.WebhookSecretRef.Key),
			corev1.EnvVar{Name: "SLACK_CHANNEL", Value: n.Slack.Channel},
			corev1.EnvVar{Name: "SLACK_USERNAME", Value: n.Slack.Username},
			corev1.EnvVar{Name: "SLACK_NOTIFY_SUCCESS", Value: strconv.FormatBool(n.NotifySuccess)},
		)
	}
	if n.Discord != nil {
		out = append(out,
			fromSecret("DISCORD_WEBHOOK_URL", n.Discord.WebhookSecretRef.Name, n.Discord.WebhookSecretRef.Key),
			corev1.EnvVar{Name: "DISCORD_USERNAME", Value: n.Discord.Username},
			corev1.EnvVar{Name: "DISCORD_NOTIFY_SUCCESS", Value: strconv.FormatBool(n.NotifySuccess)},
		)
	}
	if n.Teams != nil {
		out = append(out,
			fromSecret("TEAMS_WEBHOOK_URL", n.Teams.WebhookSecretRef.Name, n.Teams.WebhookSecretRef.Key),
			corev1.EnvVar{Name: "TEAMS_NOTIFY_SUCCESS", Value: strconv.FormatBool(n.NotifySuccess)},
		)
	}
	if n.Webhook != nil {
		out = append(out,
			fromSecret("WEBHOOK_URL", n.Webhook.URLSecretRef.Name, n.Webhook.URLSecretRef.Key),
			corev1.EnvVar{Name: "WEBHOOK_NOTIFY_SUCCESS", Value: strconv.FormatBool(n.NotifySuccess)},
		)
		if n.Webhook.AuthHeaderSecretRef != nil {
			out = append(out, fromSecret("WEBHOOK_AUTH_HEADER", n.Webhook.AuthHeaderSecretRef.Name, n.Webhook.AuthHeaderSecretRef.Key))
		}
	}
	if n.Stdout {
		out = append(out, corev1.EnvVar{Name: "NOTIFY_STDOUT", Value: "true"})
	}
	return out
}

// helpers ------------------------------------------------------------

func cronLabels(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":                 "dumpscript",
		"app.kubernetes.io/managed-by":           "dumpscript-operator",
		"dumpscript.cloudscript.com.br/schedule": name,
	}
}

func restoreLabels(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":                "dumpscript",
		"app.kubernetes.io/managed-by":          "dumpscript-operator",
		"dumpscript.cloudscript.com.br/restore": name,
	}
}

func imageOrDefault(s string) string {
	if s == "" {
		return defaultImage
	}
	return s
}

func fromSecret(envName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}

func keyOr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func int32Ptr(v int32) *int32 { return &v }
