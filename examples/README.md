# OpenLoadBalancer Examples

This directory contains example configurations for common use cases.

## Examples

### Basic

- **simple.yaml** - Basic HTTP load balancer with health checks

### SSL/TLS

- **https.yaml** - HTTPS with automatic certificate management (Let's Encrypt)

### WebSocket

- **websocket.yaml** - WebSocket load balancing with sticky sessions

### Microservices

- **api-gateway.yaml** - API gateway with path-based routing for microservices

### Kubernetes

- **k8s-config.yaml** - Service discovery from Kubernetes endpoints

## Usage

1. Copy an example configuration:
   ```bash
   cp examples/basic/simple.yaml olb.yaml
   ```

2. Edit the configuration to match your environment

3. Start OpenLoadBalancer:
   ```bash
   olb start --config olb.yaml
   ```

## Testing

Test your configuration:
```bash
olb config validate olb.yaml
```

## More Examples

For more configuration options, see:
- [Configuration Guide](../docs/configuration.md)
- [Production Deployment](../docs/production-deployment.md)
