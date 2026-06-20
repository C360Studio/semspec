# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Install git and build dependencies for CGO (required by tree-sitter)
RUN apk add --no-cache git gcc musl-dev

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the semspec binary (CGO enabled for tree-sitter)
RUN CGO_ENABLED=1 GOOS=linux go build -o /semspec ./cmd/semspec

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies.
# Python + pytest needed for structural-validator to run Python checklist checks.
RUN apk add --no-cache ca-certificates git wget python3 py3-pip \
    && pip3 install --break-system-packages pytest

# Copy binary from builder
COPY --from=builder /semspec /usr/local/bin/semspec

# Copy configs directory
COPY --from=builder /build/configs /app/configs

# Create workspace directory
RUN mkdir -p /workspace

# System-wide git identity + ownership trust so semspec can run as a non-root,
# arbitrary uid (CI runs it as the host uid so /workspace files are host-owned
# and cleanable by the host-side e2e runner; see issue #258). --system writes
# /etc/gitconfig, read by every git invocation regardless of HOME, so git
# operations work for a uid with no passwd entry or home dir.
RUN git config --system user.email "semspec@semspec.dev" \
    && git config --system user.name "Semspec" \
    && git config --system --add safe.directory '*'

# Set default environment
ENV NATS_URL=nats://localhost:4222
ENV SEMSPEC_REPO_PATH=/workspace

# Expose HTTP health/metrics port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget --no-verbose --tries=1 -O- http://localhost:8080/project-manager/health || exit 1

ENTRYPOINT ["/usr/local/bin/semspec"]
CMD ["--config", "/app/configs/semspec.json", "--repo", "/workspace"]
