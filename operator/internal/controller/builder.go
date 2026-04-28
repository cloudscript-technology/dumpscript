/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package controller

import (
	"fmt"
	"strconv"
	"strings"

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
	concurrency := bs.Spec.ConcurrencyPolicy
	if concurrency == "" {
		concurrency = batchv1.ForbidConcurrent
	}
	backoff := int32Ptr(0)
	if bs.Spec.BackoffLimit != nil {
		backoff = bs.Spec.BackoffLimit
	}

	volumes, mounts := secretVolumes(bs.Spec.Storage)
	irsaVol, irsaMount := irsaVolume(bs.Spec.Storage)
	if irsaVol != nil {
		volumes = append(volumes, *irsaVol)
		mounts = append(mounts, *irsaMount)
	}
	dbVol, dbMount := databaseVolume(bs.Spec.Database)
	if dbVol != nil {
		volumes = append(volumes, *dbVol)
		mounts = append(mounts, *dbMount)
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bs.Name,
			Namespace: bs.Namespace,
			Labels:    cronLabels(bs.Name),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   bs.Spec.Schedule,
			ConcurrencyPolicy:          concurrency,
			Suspend:                    &suspend,
			SuccessfulJobsHistoryLimit: successHist,
			FailedJobsHistoryLimit:     failHist,
			StartingDeadlineSeconds:    bs.Spec.StartingDeadlineSeconds,
			JobTemplate: batchv1.JobTemplateSpec{
				// Labels on the Job object (not just the Pod) let refreshStatus
				// find and correlate Jobs back to their BackupSchedule.
				ObjectMeta: metav1.ObjectMeta{Labels: cronLabels(bs.Name)},
				Spec: batchv1.JobSpec{
					BackoffLimit:          backoff,
					ActiveDeadlineSeconds: bs.Spec.ActiveDeadlineSeconds,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: cronLabels(bs.Name)},
						Spec: corev1.PodSpec{
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							ServiceAccountName: bs.Spec.ServiceAccountName,
							Volumes:            volumes,
							ImagePullSecrets:   bs.Spec.ImagePullSecrets,
							NodeSelector:       bs.Spec.NodeSelector,
							Tolerations:        bs.Spec.Tolerations,
							Affinity:           bs.Spec.Affinity,
							PriorityClassName:  bs.Spec.PriorityClassName,
							Containers: []corev1.Container{{
								Name:            "dumpscript",
								Image:           imageOrDefault(bs.Spec.Image),
								ImagePullPolicy: bs.Spec.ImagePullPolicy,
								Args:            []string{"dump"},
								Env: mergeEnv(
									append(buildEnv(bs.Spec.Database, bs.Spec.Storage, bs.Spec.Notifications, bs.Spec.Periodicity, bs.Spec.RetentionDays),
										scheduleRuntimeEnv(bs)...),
									bs.Spec.ExtraEnv),
								Resources:    bs.Spec.Resources,
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
	backoff := int32Ptr(0)
	if r.Spec.BackoffLimit != nil {
		backoff = r.Spec.BackoffLimit
	}
	env := buildEnv(r.Spec.Database, r.Spec.Storage, r.Spec.Notifications, "", 0)
	env = append(env, corev1.EnvVar{Name: "S3_KEY", Value: r.Spec.SourceKey})
	if r.Spec.CreateDB {
		env = append(env, corev1.EnvVar{Name: "CREATE_DB", Value: "true"})
	}
	env = append(env, restoreRuntimeEnv(r)...)
	env = mergeEnv(env, r.Spec.ExtraEnv)
	volumes, mounts := secretVolumes(r.Spec.Storage)
	irsaVol, irsaMount := irsaVolume(r.Spec.Storage)
	if irsaVol != nil {
		volumes = append(volumes, *irsaVol)
		mounts = append(mounts, *irsaMount)
	}
	dbVol, dbMount := databaseVolume(r.Spec.Database)
	if dbVol != nil {
		volumes = append(volumes, *dbVol)
		mounts = append(mounts, *dbMount)
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restore-" + r.Name,
			Namespace: r.Namespace,
			Labels:    restoreLabels(r.Name),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            backoff,
			ActiveDeadlineSeconds:   r.Spec.ActiveDeadlineSeconds,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: restoreLabels(r.Name)},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: r.Spec.ServiceAccountName,
					Volumes:            volumes,
					ImagePullSecrets:   r.Spec.ImagePullSecrets,
					NodeSelector:       r.Spec.NodeSelector,
					Tolerations:        r.Spec.Tolerations,
					Affinity:           r.Spec.Affinity,
					PriorityClassName:  r.Spec.PriorityClassName,
					Containers: []corev1.Container{{
						Name:            "dumpscript",
						Image:           imageOrDefault(r.Spec.Image),
						ImagePullPolicy: r.Spec.ImagePullPolicy,
						Args:            []string{"restore"},
						Env:             env,
						Resources:       r.Spec.Resources,
						VolumeMounts:    mounts,
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

// scheduleRuntimeEnv assembles env vars sourced from BackupScheduleSpec-level
// runtime fields (dryRun, compression, retries, etc.). Kept separate from
// buildEnv so RestoreSpec can have its own variant with overlapping but
// distinct fields (no DumpRetry on Restore, etc.).
func scheduleRuntimeEnv(bs *dumpscriptv1alpha1.BackupSchedule) []corev1.EnvVar {
	var out []corev1.EnvVar
	if bs.Spec.DryRun {
		out = append(out, corev1.EnvVar{Name: "DRY_RUN", Value: "true"})
	}
	if bs.Spec.Compression != "" {
		out = append(out, corev1.EnvVar{Name: "COMPRESSION_TYPE", Value: bs.Spec.Compression})
	}
	if bs.Spec.DumpTimeout != nil {
		out = append(out, corev1.EnvVar{Name: "DUMP_TIMEOUT", Value: bs.Spec.DumpTimeout.Duration.String()})
	}
	if bs.Spec.LockGracePeriod != nil {
		out = append(out, corev1.EnvVar{Name: "LOCK_GRACE_PERIOD", Value: bs.Spec.LockGracePeriod.Duration.String()})
	}
	if bs.Spec.WorkDir != "" {
		out = append(out, corev1.EnvVar{Name: "WORK_DIR", Value: bs.Spec.WorkDir})
	}
	if bs.Spec.LogLevel != "" {
		out = append(out, corev1.EnvVar{Name: "LOG_LEVEL", Value: bs.Spec.LogLevel})
	}
	if bs.Spec.LogFormat != "" {
		out = append(out, corev1.EnvVar{Name: "LOG_FORMAT", Value: bs.Spec.LogFormat})
	}
	if bs.Spec.VerifyContent != nil {
		out = append(out, corev1.EnvVar{Name: "VERIFY_CONTENT", Value: strconv.FormatBool(*bs.Spec.VerifyContent)})
	}
	if bs.Spec.MetricsListen != "" {
		out = append(out, corev1.EnvVar{Name: "METRICS_LISTEN", Value: bs.Spec.MetricsListen})
	}
	if r := bs.Spec.DumpRetry; r != nil {
		if r.MaxAttempts > 0 {
			out = append(out, corev1.EnvVar{Name: "DUMP_RETRIES", Value: strconv.Itoa(int(r.MaxAttempts))})
		}
		if r.InitialBackoff != nil {
			out = append(out, corev1.EnvVar{Name: "DUMP_RETRY_BACKOFF", Value: r.InitialBackoff.Duration.String()})
		}
		if r.MaxBackoff != nil {
			out = append(out, corev1.EnvVar{Name: "DUMP_RETRY_MAX_BACKOFF", Value: r.MaxBackoff.Duration.String()})
		}
	}
	out = append(out, prometheusEnv(bs.Spec.Prometheus)...)
	return out
}

// restoreRuntimeEnv mirrors scheduleRuntimeEnv for RestoreSpec.
func restoreRuntimeEnv(r *dumpscriptv1alpha1.Restore) []corev1.EnvVar {
	var out []corev1.EnvVar
	if r.Spec.DryRun {
		out = append(out, corev1.EnvVar{Name: "DRY_RUN", Value: "true"})
	}
	if r.Spec.Compression != "" {
		out = append(out, corev1.EnvVar{Name: "COMPRESSION_TYPE", Value: r.Spec.Compression})
	}
	if r.Spec.RestoreTimeout != nil {
		out = append(out, corev1.EnvVar{Name: "RESTORE_TIMEOUT", Value: r.Spec.RestoreTimeout.Duration.String()})
	}
	if r.Spec.WorkDir != "" {
		out = append(out, corev1.EnvVar{Name: "WORK_DIR", Value: r.Spec.WorkDir})
	}
	if r.Spec.LogLevel != "" {
		out = append(out, corev1.EnvVar{Name: "LOG_LEVEL", Value: r.Spec.LogLevel})
	}
	if r.Spec.LogFormat != "" {
		out = append(out, corev1.EnvVar{Name: "LOG_FORMAT", Value: r.Spec.LogFormat})
	}
	if r.Spec.VerifyContent != nil {
		out = append(out, corev1.EnvVar{Name: "VERIFY_CONTENT", Value: strconv.FormatBool(*r.Spec.VerifyContent)})
	}
	if r.Spec.MetricsListen != "" {
		out = append(out, corev1.EnvVar{Name: "METRICS_LISTEN", Value: r.Spec.MetricsListen})
	}
	out = append(out, prometheusEnv(r.Spec.Prometheus)...)
	return out
}

// prometheusEnv translates a PrometheusSpec into PROMETHEUS_* env vars.
// No-op when the spec is nil or Enabled=false.
func prometheusEnv(p *dumpscriptv1alpha1.PrometheusSpec) []corev1.EnvVar {
	if p == nil || !p.Enabled {
		return nil
	}
	out := []corev1.EnvVar{{Name: "PROMETHEUS_ENABLED", Value: "true"}}
	if p.PushgatewayURL != "" {
		out = append(out, corev1.EnvVar{Name: "PROMETHEUS_PUSHGATEWAY_URL", Value: p.PushgatewayURL})
	}
	if p.JobName != "" {
		out = append(out, corev1.EnvVar{Name: "PROMETHEUS_JOB_NAME", Value: p.JobName})
	}
	if p.Instance != "" {
		out = append(out, corev1.EnvVar{Name: "PROMETHEUS_INSTANCE", Value: p.Instance})
	}
	if p.LogOnExit {
		out = append(out, corev1.EnvVar{Name: "PROMETHEUS_LOG_ON_EXIT", Value: "true"})
	}
	return out
}

// engineVersionEnv translates the per-engine version sub-blocks
// (PostgreSQL.Version, MySQL.Version, MariaDB.Version) into POSTGRES_VERSION /
// MYSQL_VERSION / MARIADB_VERSION env vars. No-op when neither sub-block is
// set.
func engineVersionEnv(db dumpscriptv1alpha1.DatabaseSpec) []corev1.EnvVar {
	var out []corev1.EnvVar
	if db.PostgreSQL != nil && db.PostgreSQL.Version != "" {
		out = append(out, corev1.EnvVar{Name: "POSTGRES_VERSION", Value: db.PostgreSQL.Version})
	}
	if db.MySQL != nil && db.MySQL.Version != "" {
		out = append(out, corev1.EnvVar{Name: "MYSQL_VERSION", Value: db.MySQL.Version})
	}
	if db.MariaDB != nil && db.MariaDB.Version != "" {
		out = append(out, corev1.EnvVar{Name: "MARIADB_VERSION", Value: db.MariaDB.Version})
	}
	return out
}

// buildEnv translates the typed CR fields into the env-var contract that
// dumpscript itself reads (see internal/config/config.go).
func buildEnv(db dumpscriptv1alpha1.DatabaseSpec, s dumpscriptv1alpha1.StorageSpec, n *dumpscriptv1alpha1.NotificationsSpec, periodicity string, retentionDays int32) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "DB_TYPE", Value: db.Type},
		{Name: "DB_HOST", Value: db.Host},
		{Name: "DB_NAME", Value: db.Name},
	}
	env = append(env, engineVersionEnv(db)...)
	// DUMP_OPTIONS may carry tokens (e.g. Elasticsearch --auth-header=Bearer
	// xxx). Prefer the SecretKeyRef variant when set; fall back to the plain
	// field for non-sensitive flags. MongoDB.AuthSource is appended to the
	// plain options string when no SecretRef is in play (since we cannot merge
	// with a Secret value at admission time).
	extras := mongoExtras(db)
	switch {
	case db.OptionsSecretRef != nil:
		env = append(env, fromSecret("DUMP_OPTIONS", db.OptionsSecretRef.Name, db.OptionsSecretRef.Key))
	case db.Options != "" || extras != "":
		combined := strings.TrimSpace(strings.TrimSpace(db.Options) + " " + extras)
		env = append(env, corev1.EnvVar{Name: "DUMP_OPTIONS", Value: combined})
	}
	port := db.Port
	if port == 0 {
		port = defaultPort(db.Type)
	}
	if port != 0 {
		env = append(env, corev1.EnvVar{Name: "DB_PORT", Value: strconv.Itoa(int(port))})
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
		if s.S3.SSE != "" {
			env = append(env, corev1.EnvVar{Name: "S3_SSE", Value: s.S3.SSE})
			if s.S3.SSEKMSKeyID != "" {
				env = append(env, corev1.EnvVar{Name: "S3_SSE_KMS_KEY_ID", Value: s.S3.SSEKMSKeyID})
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

// mergeEnv appends user-supplied ExtraEnv to the operator-managed env list,
// dropping any user entries whose Name collides with an operator-managed key
// (the operator-managed contract wins to keep the dumpscript binary's contract
// stable).
func mergeEnv(managed, extra []corev1.EnvVar) []corev1.EnvVar {
	if len(extra) == 0 {
		return managed
	}
	taken := make(map[string]struct{}, len(managed))
	for i := range managed {
		taken[managed[i].Name] = struct{}{}
	}
	out := managed
	for i := range extra {
		if _, ok := taken[extra[i].Name]; ok {
			continue
		}
		out = append(out, extra[i])
	}
	return out
}

// defaultPort returns the well-known port for an engine, used when
// spec.database.port is left at zero. Returns 0 for engines without a
// default network port (sqlite).
func defaultPort(t string) int32 {
	switch t {
	case "postgresql":
		return 5432
	case "mysql", "mariadb":
		return 3306
	case "mongodb":
		return 27017
	case "redis":
		return 6379
	case "etcd":
		return 2379
	case "sqlserver":
		return 1433
	case "oracle":
		return 1521
	case "elasticsearch":
		return 9200
	case "clickhouse":
		return 9000
	case "neo4j":
		return 7687
	case "cockroach":
		return 26257
	}
	return 0
}

// mongoExtras returns engine-specific CLI flags derived from typed sub-fields
// that get appended to DUMP_OPTIONS. Currently only MongoDB.AuthSource is
// translated; other engines may grow similar typed fields over time.
func mongoExtras(db dumpscriptv1alpha1.DatabaseSpec) string {
	if db.Type != "mongodb" || db.MongoDB == nil || db.MongoDB.AuthSource == "" {
		return ""
	}
	return "--authenticationDatabase=" + db.MongoDB.AuthSource
}

// databaseVolume turns DatabaseSpec.Volume into a corev1.Volume +
// corev1.VolumeMount the operator can wire into the Pod spec. Returns
// nil/nil when the user did not request a volume, mirroring the irsaVolume /
// secretVolumes pattern.
func databaseVolume(db dumpscriptv1alpha1.DatabaseSpec) (*corev1.Volume, *corev1.VolumeMount) {
	if db.Volume == nil || db.Volume.MountPath == "" {
		return nil, nil
	}
	src := corev1.VolumeSource{}
	switch {
	case db.Volume.PersistentVolumeClaim != nil:
		src.PersistentVolumeClaim = db.Volume.PersistentVolumeClaim
	case db.Volume.EmptyDir != nil:
		src.EmptyDir = db.Volume.EmptyDir
	case db.Volume.ConfigMap != nil:
		src.ConfigMap = db.Volume.ConfigMap
	case db.Volume.Secret != nil:
		src.Secret = db.Volume.Secret
	default:
		return nil, nil
	}
	const name = "database-volume"
	return &corev1.Volume{Name: name, VolumeSource: src},
		&corev1.VolumeMount{Name: name, MountPath: db.Volume.MountPath}
}
