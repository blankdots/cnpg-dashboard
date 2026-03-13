# Tilt + kind local dev: build image, load into kind, deploy with Helm.
# Prereqs: kind cluster (make kind-create)

load('ext://helm_resource', 'helm_repo', 'helm_resource')

# cert-manager installs many CRDs; default 30s apply timeout is too short
update_settings(k8s_upsert_timeout_secs=180)

# ── cert-manager (required by Barman Cloud plugin) ───────────────────────────
helm_repo('jetstack', 'https://charts.jetstack.io')
helm_resource(
  'cert-manager',
  'jetstack/cert-manager',
  release_name='cert-manager',
  namespace='cert-manager',
  flags=['--create-namespace', '--set', 'crds.enabled=true'],
  labels=['cert-manager'],
)

# ── CloudNativePG (operator, barman, example cluster) ─────────────────────────
helm_repo('cnpg', 'https://cloudnative-pg.github.io/charts')
helm_resource(
  'cnpg-operator',
  'cnpg/cloudnative-pg',
  release_name='cnpg',
  namespace='cnpg-system',
  resource_deps=['cert-manager'],
  flags=['--create-namespace'],
  labels=['cnpg'],
)

helm_resource(
  'barman-cloud',
  'cnpg/plugin-barman-cloud',
  release_name='barman-cloud',
  namespace='cnpg-system',
  resource_deps=['cert-manager', 'cnpg-operator'],
  labels=['cnpg'],
)

local_resource(
  'apply-cluster-example',
  cmd='kubectl apply -f dev/cluster-example.yaml',
  resource_deps=['cnpg-operator'],
  labels=['cnpg'],
)

# ── RustFS (S3-compatible object store for Barman backups) ─────────────────────
helm_repo('rustfs', 'https://rustfs.github.io/helm/')
helm_resource(
  'rustfs-helm',
  'rustfs/rustfs',
  release_name='rustfs',
  namespace='rustfs',
  flags=['--create-namespace', '-f', 'dev/rustfs-values.yaml'],
  labels=['rustfs'],
)

# Apply RustFS credentials Secret used by both Cluster backup and ObjectStore
local_resource(
  'apply-rustfs-credentials',
  cmd='kubectl apply -f dev/rustfs-credentials.yaml',
  resource_deps=['rustfs-helm'],
  labels=['rustfs'],
)

# Apply plugin ObjectStore (rustfs-s3) so it appears in the dashboard Barman Stores tab
local_resource(
  'apply-rustfs-objectstore',
  cmd='kubectl apply -f dev/objectstore-rustfs-s3.yaml',
  resource_deps=['barman-cloud', 'apply-rustfs-credentials'],
  labels=['rustfs'],
)

# Schedule daily backups for cluster-example using its barmanObjectStore (RustFS via S3)
local_resource(
  'apply-scheduled-backup-cluster-example',
  cmd='kubectl apply -f dev/scheduledbackup-cluster-example.yaml',
  resource_deps=['cnpg-operator', 'apply-cluster-example'],
  labels=['cnpg'],
)

# ── metrics-server (CPU/memory per instance; kind needs --kubelet-insecure-tls) ─
helm_repo('metrics-server', 'https://kubernetes-sigs.github.io/metrics-server/')
helm_resource(
  'ms',
  'metrics-server/metrics-server',
  release_name='metrics-server',
  namespace='kube-system',
  flags=['--create-namespace', '-f', 'dev/metrics-server-values.yaml'],
  labels=['metrics'],
)

# ── cert-manager: issuer + dashboard cert (after CRDs) ───────────────────────
local_resource(
  'apply-cert-resources',
  cmd='kubectl wait --for=condition=established crd/certificates.cert-manager.io crd/clusterissuers.cert-manager.io --timeout=120s && kubectl apply -f dev/selfsigned-issuer.yaml -f dev/dashboard-cert.yaml',
  resource_deps=['cert-manager'],
  labels=['cert-manager'],
)

docker_build(
  'blankdots/cnpg-dashboard:latest',
  '.',
  dockerfile='Dockerfile',
)

# Load dashboard image into kind when files change (no resource_deps: image ref contains :/ so Tilt has no resource name for it)
local_resource(
  'load-kind',
  cmd='ref=$(tilt dump image-deploy-ref blankdots/cnpg-dashboard:latest 2>/dev/null) && [ -n "$ref" ] && kind load docker-image "$ref" --name cnpg-dashboard || true',
  deps=['Dockerfile', 'go.mod', 'cmd/', 'internal/', 'frontend/'],
  labels=['dashboard'],
)

# ── Redis (in-cluster store for Tilt dev; optional for production) ─────────────
k8s_yaml('dev/redis.yaml')
k8s_resource('redis', labels=['redis', 'dashboard'])

# ── Dashboard (app + Helm deploy; waits for cert-manager CRDs and CNPG) ──────
k8s_yaml(
  helm(
    'charts/cnpg-dashboard',
    name='cnpg-dashboard',
    namespace='default',
    values=['dev/values-tilt.yaml'],
    set=[
      'image.repository=blankdots/cnpg-dashboard',
      'image.tag=latest',
      'image.pullPolicy=Never',
    ],
  ),
)

k8s_resource(
  'cnpg-dashboard',
  resource_deps=['apply-cert-resources', 'cnpg-operator', 'redis'],
  port_forwards=['8443:8443'],
  labels=['dashboard'],
)
