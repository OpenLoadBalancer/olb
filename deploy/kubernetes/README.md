# Kubernetes Deployment

Deploy OpenLoadBalancer to a Kubernetes cluster using the static manifests or the Helm chart.

## Prerequisites

- Kubernetes 1.24+
- `kubectl` configured to access your cluster
- Container image `openloadbalancer/olb` available in a registry

## Quick Start

Apply all manifests in order:

```bash
kubectl apply -f configmap.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

Verify the deployment:

```bash
kubectl get pods -l app.kubernetes.io/name=olb
kubectl get svc olb
```

## Configuration

Edit `configmap.yaml` to match your environment. The config is mounted at `/etc/olb/olb.yaml` inside the container. After editing, restart the pods to pick up changes:

```bash
kubectl rollout restart deployment olb
```

## Scaling

```bash
kubectl scale deployment olb --replicas=5
```

## Cleanup

```bash
kubectl delete -f service.yaml
kubectl delete -f deployment.yaml
kubectl delete -f configmap.yaml
```

## Helm Chart

For a templated deployment with configurable values, use the Helm chart instead:

```bash
helm install olb ../../helm/olb
```

See `../../helm/olb/values.yaml` for all configurable options.
