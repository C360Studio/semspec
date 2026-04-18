# qa-runner.Dockerfile
#
# TRUST BOUNDARY (read before modifying):
#   qa-runner runs TRUSTED orchestration code (ours). It mounts the Docker
#   socket so it can invoke 'act', which spawns short-lived containers that
#   execute .github/workflows/qa.yml steps.
#
#   Agent-authored code (test files, app code) runs INSIDE containers that
#   act spawns — it does NOT run in the qa-runner process itself.
#
#   This separation is why qa-runner holds the Docker socket and semspec does
#   not: qa-runner's blast radius is its own orchestration logic, which we
#   control. The agent-code blast radius is bounded by the act-spawned
#   containers and whatever resource limits they carry.
#
# Stage 1: Build qa-runner binary from the semspec module.
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /qa-runner ./cmd/qa-runner

# Stage 2: Runtime image with Docker CLI and act for workflow execution.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Core system tools + Docker client.
# We install docker-ce-cli only — we mount the host daemon's socket, we do
# NOT run dockerd inside the container. The host daemon is the one spawning
# act's job containers.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    git \
    unzip \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
        -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
        https://download.docker.com/linux/ubuntu \
        $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
        > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli \
    && rm -rf /var/lib/apt/lists/*

# Install act — pinned release for reproducibility. Bump ARG_ACT_VERSION to
# upgrade. We download the pre-built binary directly from the GitHub release
# rather than using the install script so the version is locked in the image
# layer and rebuilds are cache-friendly.
ARG ACT_VERSION=v0.2.70
ARG TARGETARCH
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
        amd64) ACT_ARCH="x86_64" ;; \
        arm64) ACT_ARCH="arm64"  ;; \
        *)     ACT_ARCH="x86_64" ;; \
    esac; \
    curl -fsSL \
        "https://github.com/nektos/act/releases/download/${ACT_VERSION}/act_Linux_${ACT_ARCH}.tar.gz" \
        | tar -C /usr/local/bin -xz act \
    && chmod +x /usr/local/bin/act \
    && act --version

# User strategy: run as root inside the container.
#
# Rationale: qa-runner already holds a privileged socket (/var/run/docker.sock).
# The meaningful security boundary is the socket mount policy enforced at the
# host / compose level — not the in-container UID. Running root avoids the
# docker group membership dance (the socket is typically owned by root:docker
# on the host, and the GID varies across distros). This matches how most
# act-based CI runners operate in self-hosted environments.
#
# If your deployment requires a non-root user, add:
#   ARG QA_RUNNER_UID=1000
#   ARG QA_RUNNER_GID=1000
#   RUN groupadd -g ${QA_RUNNER_GID} qa-runner \
#       && useradd -m -u ${QA_RUNNER_UID} -g ${QA_RUNNER_GID} qa-runner \
#       && groupadd docker || true \
#       && usermod -aG docker qa-runner
#   USER qa-runner
# and ensure the host docker socket's GID matches.

COPY --from=builder /qa-runner /usr/local/bin/qa-runner

WORKDIR /workspace
EXPOSE 8091

ENTRYPOINT ["qa-runner"]
CMD ["--addr", ":8091"]
