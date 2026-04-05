# OpenLoadBalancer Helm Chart

## Prerequisites

- Kubernetes 1.25+
- Helm 3.12+

## Installation

```bash
# Add repo (when published)
helm repo add olb https://charts.openloadbalancer.dev
helm repo update

# Install
helm install my-olb olb/olb
```

## Local Installation

```bash
# From repo root
helm install my-olb ./helm/olb

# With custom values
helm install my-olb ./helm/olb -f my-values.yaml
```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `2` |
| `image.repository` | Image repository | `openloadbalancer/olb` |
| `image.tag` | Image tag | `latest` |
| `service.type` | Service type | `LoadBalancer` |
| `autoscaling.enabled` | Enable HPA | `false` |
| `cluster.enabled` | Enable clustering | `false` |
| `waf.enabled` | Enable WAF | `true` |

## Cluster Mode

```yaml
cluster:
  enabled: true
  join:
    - olb-0.olb-headless:7946
    - olb-1.olb-headless:7946
```

## TLS with cert-manager

```yaml
config:
  listeners:
    - name: https
      protocol: https
      address: ":443"
      tls:
        certManager: true
        certManagerIssuer: letsencrypt-prod
```

## Uninstall

```bash
helm uninstall my-olb
```
