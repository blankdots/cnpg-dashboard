# cnpg-dashboard Helm Chart

Web UI for [CloudNativePG](https://cloudnative-pg.io/) clusters and Barman object stores. View clusters, instance status, trigger backups, switchover, and (with metrics-server) CPU/memory per instance.

## Requirements

- Kubernetes >= 1.26
- [CloudNativePG operator](https://cloudnative-pg.io/documentation/current/installation_upgrade/) installed (Cluster CRD)
- Optional: [cert-manager](https://cert-manager.io/) for TLS (wss://)
- Optional: [metrics-server](https://github.com/kubernetes-sigs/metrics-server) for CPU/memory in the UI
- Optional: [Gateway API](https://gateway-api.sigs.k8s.io/) for HTTPRoute ingress

## Install

```bash
helm repo add cnpg-dashboard https://blankdots.github.io/cnpg-dashboard  # if published
helm install cnpg-dashboard ./charts/cnpg-dashboard -n cnpg-dashboard --create-namespace
```

Or from the repo root:

```bash
helm install cnpg-dashboard charts/cnpg-dashboard -n cnpg-dashboard --create-namespace
```

## Configuration

| Value | Description | Default |
|-------|-------------|---------|
| `image.repository` | Image repository | `ghcr.io/blankdots/cnpg-dashboard` |
| `image.tag` | Image tag | Chart `appVersion` |
| `service.port` | Service port (HTTP) | `8080` |
| `tls.enabled` | Enable TLS (HTTPS + wss) | `false` |
| `tls.secretName` | Secret with `tls.crt` / `tls.key` | `""` |
| `tls.port` | HTTPS port | `8443` |
| `gatewayApi.enabled` | Create HTTPRoute for Gateway API | `false` |
| `gatewayApi.gateway.name` | Gateway name to attach route to | `""` |
| `gatewayApi.tls.listenerName` | HTTPS listener name (e.g. `https`) | `""` |
| `log.level` | Log level | `info` |
| `log.format` | `json` or `text` | `json` |

### TLS (cert-manager)

With cert-manager installed, enable TLS and let the chart create Certificate + ClusterIssuer:

```yaml
tls:
  enabled: true
  secretName: cnpg-dashboard-tls
# Optionally skip cert resources and create them yourself:
#  skipCertResources: true
```

### Gateway API

To expose the dashboard via an existing Gateway (e.g. with TLS):

```yaml
gatewayApi:
  enabled: true
  gateway:
    name: my-gateway
    namespace: ""   # same as release namespace if empty
  hostnames:
    - cnpg-dashboard.example.com
  tls:
    listenerName: "https"
```

## Uninstall

```bash
helm uninstall cnpg-dashboard -n cnpg-dashboard
```
