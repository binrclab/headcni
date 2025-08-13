# Multi-stage build for HeadCNI
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN make build

# Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    iptables \
    iproute2 \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 headcni && \
    adduser -D -s /bin/sh -u 1000 -G headcni headcni

# Create necessary directories
RUN mkdir -p /opt/cni/bin /etc/cni/net.d /var/lib/tailscale /var/run/tailscale && \
    chown -R headcni:headcni /opt/cni /etc/cni /var/lib/tailscale /var/run/tailscale

# Copy binaries from builder
COPY --from=builder /app/headcni /opt/cni/bin/
COPY --from=builder /app/headcni-ipam /opt/cni/bin/
COPY --from=builder /app/bin/headcni-node /usr/local/bin/ 2>/dev/null || true

# Copy CNI configuration
COPY --from=builder /app/10-headcni.conflist /etc/cni/net.d/
COPY --from=builder /app/10-headcni-ipam.conflist /etc/cni/net.d/

# Set permissions
RUN chmod +x /opt/cni/bin/headcni /opt/cni/bin/headcni-ipam /usr/local/bin/headcni-node

# Switch to non-root user
USER headcni

# Set working directory
WORKDIR /app

# Expose ports
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

# Default command
CMD ["/usr/local/bin/headcni-node"] 