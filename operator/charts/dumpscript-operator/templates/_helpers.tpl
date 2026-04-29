{{/*
Standard chart name. Truncated to 63 chars to fit Kubernetes name limits.
*/}}
{{- define "dumpscript-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name: <release>-<chart>, used for resource names so
multiple releases in the same namespace don't collide.
*/}}
{{- define "dumpscript-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "dumpscript-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard recommended labels. Applied to all resources for selector
clarity and Prometheus / kube-state-metrics joins.
*/}}
{{- define "dumpscript-operator.labels" -}}
helm.sh/chart: {{ include "dumpscript-operator.chart" . }}
{{ include "dumpscript-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
control-plane: controller-manager
{{- end -}}

{{- define "dumpscript-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dumpscript-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name to use. If create=false, the user MUST set name to an
existing SA — we don't fall back silently to "default" because that would
be a security footgun (operator gets default SA's RBAC).
*/}}
{{- define "dumpscript-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "dumpscript-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create=false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference. Prefer digest over tag for reproducible deploys; fall
back to tag, falling back to .Chart.AppVersion if neither is set.
*/}}
{{- define "dumpscript-operator.image" -}}
{{- $repo := .Values.image.repository -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" $repo .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" $repo (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}
{{- end -}}
