# Dev setup: RustFS with S3 for CNPG backups

RustFS runs in-cluster and exposes an S3-compatible API. CloudNativePG uses it via the Barman Cloud plugin for WAL archiving and base backups.

## With Tilt (recommended)

From the repo root:

```bash
make kind-create   # or create a kind cluster
tilt up
```

Tilt will:

1. Install **cert-manager**, **CloudNativePG**, and the **plugin-barman-cloud** in their namespaces.
2. Install **RustFS** in namespace `rustfs` (Helm release `rustfs`, service **`rustfs-svc`** on ports 9000/9001).
3. Apply **rustfs-credentials** (Secret in `default`) and the **ObjectStore** `rustfs-s3` that points at RustFS.
4. Apply **cluster-example** (plugin `barmanObjectName: rustfs-s3`) and the **ScheduledBackup** for daily backups.

Backups and WAL go to RustFS at `s3://cnpg-backups/` (bucket `cnpg-backups`). Create the bucket in RustFS if it doesn’t exist (e.g. via RustFS UI or S3 client).

## Endpoint and credentials

| Item | Value |
|------|--------|
| **RustFS S3 endpoint** | `http://rustfs-svc.rustfs.svc.cluster.local:9000` |
| **Service name** | `rustfs-svc` (namespace `rustfs`) – not `rustfs` |
| **Default credentials** | `rustfsadmin` / `rustfsadmin` (see `rustfs-credentials.yaml`) |
| **ObjectStore name** | `rustfs-s3` (referenced by cluster as `barmanObjectName: rustfs-s3`) |

## Manual apply order (without Tilt)

1. Create cluster (kind/k3s, etc.) and install **cert-manager**, **CloudNativePG**, **plugin-barman-cloud**.
2. Install RustFS:  
   `helm install rustfs rustfs/rustfs -n rustfs --create-namespace -f dev/rustfs-values.yaml`
3. Apply `dev/rustfs-credentials.yaml`, then `dev/objectstore-rustfs-s3.yaml`.
4. Apply `dev/cluster-example.yaml`, then `dev/scheduledbackup-cluster-example.yaml`.

## Verify

```bash
# RustFS reachable from default namespace
kubectl run curl-test --rm -it --restart=Never --image=curlimages/curl -- \
  curl -s -o /dev/null -w "%{http_code}\n" http://rustfs-svc.rustfs.svc.cluster.local:9000
# Expect 403 (no auth) or 200; not 000.

# Cluster and backups
kubectl get cluster cluster-example
kubectl get backups.postgresql.cnpg.io
kubectl get objectstore rustfs-s3
```

## Files

| File | Purpose |
|------|---------|
| `rustfs-values.yaml` | Helm values for RustFS (standalone, StorageClass, ingress class). |
| `rustfs-credentials.yaml` | Secret with `ACCESS_KEY_ID` / `SECRET_ACCESS_KEY` for RustFS. |
| `objectstore-rustfs-s3.yaml` | Barman Cloud ObjectStore pointing at RustFS S3 API. |
| `cluster-example.yaml` | CNPG cluster with plugin `barmanObjectName: rustfs-s3`. |
| `scheduledbackup-cluster-example.yaml` | ScheduledBackup (method: plugin) for daily backups. |
