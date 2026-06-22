# Stage 1: Build sandbox binary from the semspec module.
FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -o /sandbox ./cmd/sandbox

# Stage 2: Runtime image with the toolchains agents need to build and test code.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Core system tools.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    git \
    curl \
    wget \
    jq \
    ca-certificates \
    unzip \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Go — match the version required by the project.
ARG GO_VERSION=1.25.3
ARG TARGETARCH
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/go/bin:${PATH}"
ENV GOPATH=/go
ENV GOMODCACHE=/go/pkg/mod

# Java JDK + Maven (semsource Java AST support + Gradle/Maven builds).
# Maven added 2026-05-05 — @hard fixture (osh-driver-meshtastic) is a
# Maven project; without `mvn` the dev loop bashes `mvn test` and gets
# `mvn: not found`, then has no way to validate the Java it wrote.
RUN apt-get update && apt-get install -y --no-install-recommends \
    openjdk-21-jdk-headless \
    maven \
    && rm -rf /var/lib/apt/lists/* \
    && ln -s /usr/lib/jvm/java-21-openjdk-* /usr/lib/jvm/java-21
ENV JAVA_HOME=/usr/lib/jvm/java-21
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# Gradle.
ARG GRADLE_VERSION=8.12
RUN curl -fsSL "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" \
    -o /tmp/gradle.zip \
    && unzip -q /tmp/gradle.zip -d /opt \
    && rm /tmp/gradle.zip \
    && ln -s "/opt/gradle-${GRADLE_VERSION}/bin/gradle" /usr/local/bin/gradle
ENV GRADLE_HOME="/opt/gradle-${GRADLE_VERSION}"

# Node.js 22 LTS + global TypeScript tooling.
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g typescript vitest \
    && rm -rf /var/lib/apt/lists/*

# Docker CLI — DooD pattern. Sandbox uses the host docker daemon via the
# /var/run/docker.sock mount declared in compose. Lets the dev's TDD loop
# spawn real integration-test containers (Testcontainers, raw docker run
# from gradle/maven build scripts, etc.) without a nested daemon. Trust
# boundary is enforced at the tool-call governance layer — escape shapes
# like `docker run --privileged`, host-root mounts, and nested socket
# mounts are denied via configs/e2e-hybrid.json rule-processor inline_rules.
ARG DOCKER_CLI_VERSION=27.5.1
RUN ARCH=$(uname -m) \
    && curl -fsSL "https://download.docker.com/linux/static/stable/${ARCH}/docker-${DOCKER_CLI_VERSION}.tgz" \
        -o /tmp/docker.tgz \
    && tar -xzf /tmp/docker.tgz -C /tmp \
    && mv /tmp/docker/docker /usr/local/bin/docker \
    && rm -rf /tmp/docker /tmp/docker.tgz

# Non-root sandbox user with configurable UID/GID.
# Pass SANDBOX_UID and SANDBOX_GID at build time to match host user.
# This ensures files created inside the container are owned by the host
# user, avoiding permission issues on bind-mounted repositories.
#
# Usage: docker compose build --build-arg SANDBOX_UID=$(id -u) --build-arg SANDBOX_GID=$(id -g) sandbox
ARG SANDBOX_UID=1000
ARG SANDBOX_GID=1000
# Remove any pre-existing user/group at the target UID/GID, then create sandbox.
# Ubuntu 24.04 ships with user 'ubuntu' at UID/GID 1000 which conflicts.
RUN existing_user=$(getent passwd ${SANDBOX_UID} | cut -d: -f1) \
    && if [ -n "$existing_user" ] && [ "$existing_user" != "sandbox" ]; then userdel -r "$existing_user" 2>/dev/null || true; fi \
    && existing_group=$(getent group ${SANDBOX_GID} | cut -d: -f1) \
    && if [ -n "$existing_group" ] && [ "$existing_group" != "sandbox" ]; then groupdel "$existing_group" 2>/dev/null || true; fi \
    && groupadd -g ${SANDBOX_GID} sandbox \
    && useradd -m -s /bin/bash -u ${SANDBOX_UID} -g ${SANDBOX_GID} sandbox \
    && mkdir -p /go/pkg/mod \
    && chown -R sandbox:sandbox /go

COPY --from=builder /sandbox /usr/local/bin/sandbox
# Entrypoint wrapper: configures GitHub auth (curl + git) from GITHUB_TOKEN at
# container start so agent upstream-resolution calls don't hit the GitHub
# unauthenticated rate limit. Copied + made executable as root before USER drop.
COPY docker/sandbox-entrypoint.sh /usr/local/bin/sandbox-entrypoint.sh
RUN chmod +x /usr/local/bin/sandbox-entrypoint.sh

# Trust mounted repos regardless of host uid. /workspace is owned by the host
# user, whose uid can differ from the sandbox uid (CI runner 1001 vs sandbox
# 1000); without this, git's dubious-ownership guard aborts the sandbox's
# "ensure valid HEAD" commit (exit 128), the container never serves /health, and
# `up --wait` fails the whole stack. --system writes /etc/gitconfig as root, so
# every git invocation reads it regardless of HOME — more robust than the
# entrypoint's per-user --global (the sandbox binary may run git with a
# different HOME).
RUN git config --system --add safe.directory '*'

USER sandbox
# Explicit HOME so the entrypoint writes ~/.netrc + ~/.curlrc to the sandbox
# user's home deterministically (Docker does not always derive HOME from USER).
ENV HOME=/home/sandbox
RUN git config --global user.email "sandbox@semspec.dev" \
    && git config --global user.name "Semspec Sandbox"
# Gradle iteration speed (cold-build lever, 2026-06-13). A user-global build
# cache + warm daemon so repeated dev/validator test runs reuse compiled task
# outputs across per-task worktrees instead of recompiling unchanged upstream
# modules (e.g. osh-core) on every cycle. GRADLE_USER_HOME defaults to
# ~/.gradle, shared across worktrees, so the cache at
# ~/.gradle/caches/build-cache-1 persists for the container's life. Crucially
# org.gradle.caching helps EVEN when a build is invoked with --no-daemon (the
# cache is independent of the daemon), which is the dominant recompilation cost
# on large source_build projects. daemon/parallel only take effect when the
# agent does not force --no-daemon (the developer prompt steers it that way).
RUN mkdir -p "$HOME/.gradle" \
    && printf 'org.gradle.caching=true\norg.gradle.daemon=true\norg.gradle.parallel=true\n' \
       > "$HOME/.gradle/gradle.properties"
WORKDIR /workspace
EXPOSE 8090

ENTRYPOINT ["sandbox-entrypoint.sh"]
CMD ["--addr", ":8090", "--repo", "/workspace"]
