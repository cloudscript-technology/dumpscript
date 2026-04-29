# dumpscript-operator Helm chart

Kubernetes operator that drives scheduled database backups via the
`BackupSchedule` CRD and on-demand restores via the `Restore` CRD.
Supports PostgreSQL, MySQL, MariaDB, MongoDB, Redis, etcd, SQL Server
(and more) with S3, GCS, and Azure Blob storage backends.

## Install

```sh
helm repo add dumpscript https://cloudscript-technology.github.io/dumpscript
helm install dumpscript-operator dumpscript/dumpscript-operator \
  --namespace dumpscript-system --create-namespace
```

Or directly from the source tree:

```sh
helm install dumpscript-operator ./operator/charts/dumpscript-operator \
  --namespace dumpscript-system --create-namespace
```

## Values

| Key                              | Default                                                       | Description                                                              |
| -------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `image.repository`               | `ghcr.io/cloudscript-technology/dumpscript-operator`          | Operator image repo                                                      |
| `image.tag`                      | `""` (defaults to `.Chart.appVersion`)                        | Image tag — pin to a release for reproducibility                         |
| `image.digest`                   | `""`                                                          | If set, takes precedence over tag (preferred for production)             |
| `replicaCount`                   | `1`                                                           | Use ≥2 with `leaderElect=true` for HA                                    |
| `leaderElect`                    | `true`                                                        | Coordinate active replica via lease                                      |
| `serviceAccount.create`          | `true`                                                        | Create a dedicated SA. Set false to bring your own                       |
| `serviceAccount.name`            | `""`                                                          | Override SA name (required when `create=false`)                          |
| `serviceAccount.annotations`     | `{}`                                                          | Common use: IRSA (`eks.amazonaws.com/role-arn`) or Workload Identity     |
| `metrics.enabled`                | `true`                                                        | Expose Prometheus metrics on a Service                                   |
| `metrics.service.port`           | `8443`                                                        | Metrics port (HTTPS, auth-protected)                                     |
| `serviceMonitor.enabled`         | `false`                                                       | Create a ServiceMonitor (requires prometheus-operator)                   |
| `serviceMonitor.interval`        | `30s`                                                         | Scrape interval                                                          |
| `crds.install`                   | `true`                                                        | Helm installs CRDs from `crds/`. Disable for gitops-managed CRDs         |
| `resources`                      | 10m/64Mi requests, 500m/128Mi limits                          | Operator is mostly idle — defaults fit a small cluster                   |
| `nodeSelector` / `tolerations` / `affinity` | `{}` / `[]` / `{}`                                | Standard Kubernetes scheduling controls                                  |
| `priorityClassName`              | `""`                                                          | Pin operator to a high-priority class in production                      |

See [`values.yaml`](./values.yaml) for the full schema.

## CRD lifecycle

CRDs in `crds/` are installed by Helm on **first install only**. Helm
intentionally does not remove CRDs on `helm uninstall` (would wipe
user-created `BackupSchedule`/`Restore` resources). To upgrade CRDs
across chart versions, apply the new manifests manually:

```sh
kubectl apply -f operator/charts/dumpscript-operator/crds/
```

If you manage CRDs out-of-band (ArgoCD, gitops, operator-sdk), set
`crds.install=false`.

## Running

After install, create a `BackupSchedule`:

```yaml
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: postgres-daily
  namespace: my-app
spec:
  schedule: "0 3 * * *"
  periodicity: daily
  image: ghcr.io/cloudscript-technology/dumpscript:latest
  database:
    type: postgresql
    host: postgres.my-app.svc.cluster.local
    name: appdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: my-backups
      region: us-east-1
      credentialsSecretRef: { name: aws-credentials }
```

The operator translates this into a CronJob in the same namespace and
maintains its status (`lastJobName`, `lastSuccessTime`, conditions).

## Uninstall

```sh
helm uninstall dumpscript-operator -n dumpscript-system
# CRDs are kept; remove explicitly if desired:
kubectl delete crd backupschedules.dumpscript.cloudscript.com.br
kubectl delete crd restores.dumpscript.cloudscript.com.br
```
