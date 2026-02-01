# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the semspec binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /semspec ./cmd/semspec

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates git

# Copy binary from builder
COPY --from=builder /semspec /usr/local/bin/semspec

# Create workspace directory
RUN mkdir -p /workspace

# Set default environment
ENV NATS_URL=nats://localhost:4222
ENV SEMSPEC_REPO_PATH=/workspace

ENTRYPOINT ["/usr/local/bin/semspec"]
CMD ["--repo", "/workspace"]
