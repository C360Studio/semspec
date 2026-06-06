#!/bin/sh
# Sandbox entrypoint wrapper.
#
# When GITHUB_TOKEN is present in the environment, configure curl + git to
# authenticate to GitHub before handing control to the sandbox binary. Agent
# upstream-resolution work (e.g. the architect probing the GitHub REST API for a
# library's API surface) otherwise runs unauthenticated and hits the 60/hr rate
# limit — the rate-limited response is JSON without the searched symbol, so
# `curl -s api.github.com/... | grep <symbol>` exits 1 and the agent thrashes
# (caught on the 2026-06-06 gemini mavlink-hard run: ~1.1M tokens / 7.6 min and
# a RepeatToolFailure ALERT, all from one architect loop). With the token the
# limit is 5000/hr and the calls return real data.
#
# Defensive: only acts when the token is set; any failure here is non-fatal so a
# misconfigured credential never blocks the sandbox from starting.
set -e

if [ -n "${GITHUB_TOKEN:-}" ]; then
	HOME="${HOME:-/home/sandbox}"
	(
		umask 077
		# .netrc covers curl --netrc and git over HTTPS. x-access-token is the
		# conventional username; for a classic PAT any username + token-as-password
		# is accepted as Basic auth by the GitHub REST API.
		cat > "$HOME/.netrc" <<EOF
machine api.github.com
  login x-access-token
  password ${GITHUB_TOKEN}
machine github.com
  login x-access-token
  password ${GITHUB_TOKEN}
machine raw.githubusercontent.com
  login x-access-token
  password ${GITHUB_TOKEN}
EOF
		chmod 600 "$HOME/.netrc"
		# Make plain `curl` (no flags) consult .netrc — agents write bare
		# `curl -s <url>` and shouldn't have to remember an auth flag.
		printf -- '--netrc\n' > "$HOME/.curlrc"
		chmod 600 "$HOME/.curlrc"
		# Rewrite https github clones/fetches to carry the token too.
		git config --global url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"
	) || echo "[sandbox-entrypoint] WARN: GitHub credential setup failed; continuing unauthenticated" >&2
fi

exec sandbox "$@"
