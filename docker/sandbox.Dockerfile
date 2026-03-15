# Stage 1: Build sandbox binary from the semspec module.
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /sandbox ./cmd/sandbox

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

# Java JDK (semsource Java AST support + Gradle builds).
RUN apt-get update && apt-get install -y --no-install-recommends \
    openjdk-21-jdk-headless \
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

# Non-root sandbox user.  Give it ownership of the Go module cache so
# `go get` and `go test` work without root.
RUN useradd -m -s /bin/bash -U sandbox \
    && mkdir -p /go/pkg/mod \
    && chown -R sandbox:sandbox /go

COPY --from=builder /sandbox /usr/local/bin/sandbox

USER sandbox
WORKDIR /repo
EXPOSE 8090

ENTRYPOINT ["sandbox"]
CMD ["--addr", ":8090", "--repo", "/repo"]
