# Frontend build stage
FROM node:20-alpine AS frontend

WORKDIR /build/webui
COPY internal/webui/package.json internal/webui/package-lock.json ./
RUN npm ci
COPY internal/webui/ ./
RUN npm run build

# Go build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go.mod first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy frontend build output into the webui directory for go:embed
COPY --from=frontend /build/webui/assets/ /build/internal/webui/assets/
COPY --from=frontend /build/webui/index.html /build/internal/webui/index.html
COPY --from=frontend /build/webui/favicon.svg /build/internal/webui/favicon.svg

# Build the binary
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/openloadbalancer/olb/pkg/version.Version=$(cat VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo '0.1.0')" \
    -o bin/olb ./cmd/olb

# Runtime stage
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 olb && \
    adduser -u 1000 -G olb -s /bin/sh -D olb

# Set working directory
WORKDIR /etc/olb

# Copy binary from builder
COPY --from=builder /build/bin/olb /usr/local/bin/olb

# Copy default configs
COPY --from=builder /build/configs/ /etc/olb/configs/

# Create necessary directories
RUN mkdir -p /var/log/olb /var/lib/olb && \
    chown -R olb:olb /var/log/olb /var/lib/olb /etc/olb

# Switch to non-root user
USER olb

# Expose ports
# 80/443 - HTTP/HTTPS
# 8080 - Admin API
# 7946 - Cluster gossip
EXPOSE 80 443 8080 7946

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD /usr/local/bin/olb health || exit 1

# Default command
ENTRYPOINT ["/usr/local/bin/olb"]
CMD ["--config", "/etc/olb/configs/olb.yaml"]
