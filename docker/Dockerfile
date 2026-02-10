# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install git for go mod download
RUN apk add --no-cache git

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the semspec binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /semspec ./cmd/semspec

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates git wget

# Copy binary from builder
COPY --from=builder /semspec /usr/local/bin/semspec

# Copy configs directory
COPY --from=builder /build/configs /app/configs

# Create workspace directory
RUN mkdir -p /workspace

# Set default environment
ENV NATS_URL=nats://localhost:4222
ENV SEMSPEC_REPO_PATH=/workspace

# Expose HTTP health/metrics port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/semspec"]
CMD ["--config", "/app/configs/semspec.json", "--repo", "/workspace"]
