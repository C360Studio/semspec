#!/usr/bin/env bash
# active-poll.sh — emit one-line wedge-shape events during a paid e2e run.
#
# Pairs with the semspec watch sidecar (watch.log). The watch sidecar greps
# for crash-shaped signal (errors=N counter, RepeatToolFailure ALERTs). It
# stays SILENT through INFO-shaped events that nonetheless wedge a paid run:
# req-reviewer rejections, recovery dispatches, plan-decision auto-accepts,
# stage advances that stall. Smoke 8 (mavlink-hard 2026-06-02) burned ~75min
# because the operator polled every ~20min while the backend's per-req
# timeout was ticking down silently.
#
# This script polls authoritative state every POLL_INTERVAL seconds and
# emits one line per event into poll.log. Line-oriented so `tail -F poll.log`
# is a viable primary monitor.
#
# Emit shapes (single line, prefix = "[POLL <iso-ts>]"):
#   STAGE_CHANGE       slug=<slug> from=<old> to=<new>
#   WEDGE_DETECT       slug=<slug> stage=<stage> stuck_for=<sec>
#   REVIEWER_REJECT    slug=<slug>? line="<grep snippet>"
#   RECOVERY_DISPATCH  line="<grep snippet>"
#   PLAN_DECISION      line="<grep snippet>"
#   ERROR              line="<grep snippet>"
#   HEARTBEAT          plans=<n> active=<slug:stage,...>
#
# Usage: active-poll.sh <out_dir> [http] [container]
#   out_dir   — where to write poll.log + .poll-state/
#   http      — defaults to http://localhost:3000
#   container — defaults to ui-semspec-1
#
# Env:
#   POLL_INTERVAL (default 30s)
#   WEDGE_WARN_SECONDS (default 600) — fallback stage threshold for stages
#                                       not in the per-stage map
#   WEDGE_THRESHOLD_IMPLEMENTING (default 1800)   — implementing legitimately
#                                                     runs long; 30min before warn
#   WEDGE_THRESHOLD_GENERATING (default 900)      — generating_*/reviewing_*; 15min
#   WEDGE_THRESHOLD_DRAFTING (default 300)        — drafting; 5min

set -u

OUT_DIR="${1:?usage: active-poll.sh out_dir [http] [container]}"
HTTP="${2:-http://localhost:3000}"
CONTAINER="${3:-ui-semspec-1}"
POLL="${POLL_INTERVAL:-30}"
WEDGE_WARN="${WEDGE_WARN_SECONDS:-600}"

mkdir -p "$OUT_DIR/.poll-state"
LOG="$OUT_DIR/poll.log"

now_iso() { date -u +'%Y-%m-%dT%H:%M:%SZ'; }
now_epoch() { date -u +%s; }
emit() { printf '[POLL %s] %s\n' "$(now_iso)" "$*" >> "$LOG"; }

# wedge_threshold_for_stage returns the wedge-warn seconds for a given
# plan stage. Per smoke 9 (2026-06-02): `implementing` legitimately
# runs for an hour or more on hard fixtures, so the original universal
# 600s threshold spammed false-positive wedge warnings. Per-stage map
# keeps the signal honest — short stages get tighter thresholds, long
# stages get more headroom. Unknown stages fall back to WEDGE_WARN.
wedge_threshold_for_stage() {
	case "$1" in
		implementing)
			echo "${WEDGE_THRESHOLD_IMPLEMENTING:-1800}" ;;
		generating_*|reviewing_*|preparing_*)
			echo "${WEDGE_THRESHOLD_GENERATING:-900}" ;;
		drafting)
			echo "${WEDGE_THRESHOLD_DRAFTING:-300}" ;;
		*)
			echo "$WEDGE_WARN" ;;
	esac
}

# Track when we last scraped docker logs so we never re-emit a line.
LAST_LOG_TS_FILE="$OUT_DIR/.poll-state/last-log-epoch"
echo "$(now_epoch)" > "$LAST_LOG_TS_FILE"

# Track the highest semspec log-line timestamp we've already emitted into
# poll.log. Lines with timestamps <= this value are dropped on the next
# poll, killing the duplicate-emission shape PR #86's overlap window
# created. ISO-8601 timestamps sort correctly under bash string
# comparison so no extra parsing needed.
LAST_EMIT_TS_FILE="$OUT_DIR/.poll-state/last-emit-ts"

trap 'emit "POLL_EXIT signal=$?"' EXIT

emit "POLL_START interval=${POLL}s http=$HTTP container=$CONTAINER wedge_warn=${WEDGE_WARN}s"

# ── Helpers ──────────────────────────────────────────────────────────────

# Plan list → emit STAGE_CHANGE for any slug whose stage differs from
# the per-slug state file. Update state file. Also detect wedge (stage
# unchanged for >WEDGE_WARN seconds) and emit once per WEDGE_WARN window.
poll_plan_stages() {
	local plans_json
	plans_json="$(curl -fsS --max-time 5 "$HTTP/plan-manager/plans" 2>/dev/null || echo '[]')"

	# jq output: one line per plan, "slug|stage"
	local entries
	entries="$(echo "$plans_json" | jq -r '.[] | "\(.slug)|\(.stage)"' 2>/dev/null || true)"

	local active=""
	local n=0
	local epoch
	epoch="$(now_epoch)"

	while IFS='|' read -r slug stage; do
		[ -z "$slug" ] && continue
		n=$((n + 1))
		[ -z "$active" ] && active="$slug:$stage" || active="$active,$slug:$stage"

		local state_file="$OUT_DIR/.poll-state/stage-$slug"
		local since_file="$OUT_DIR/.poll-state/since-$slug"
		local prev_stage=""
		local since="$epoch"
		[ -f "$state_file" ] && prev_stage="$(cat "$state_file")"
		[ -f "$since_file" ] && since="$(cat "$since_file")"

		if [ "$prev_stage" != "$stage" ]; then
			emit "STAGE_CHANGE slug=$slug from=${prev_stage:-<new>} to=$stage"
			echo "$stage" > "$state_file"
			echo "$epoch" > "$since_file"
		else
			local elapsed=$((epoch - since))
			local threshold
			threshold="$(wedge_threshold_for_stage "$stage")"
			if [ "$elapsed" -gt "$threshold" ]; then
				# Emit once per threshold window to avoid log spam.
				local window=$((elapsed / threshold))
				local marker="$OUT_DIR/.poll-state/wedge-$slug-$stage-$window"
				if [ ! -f "$marker" ]; then
					touch "$marker"
					emit "WEDGE_DETECT slug=$slug stage=$stage stuck_for=${elapsed}s threshold=${threshold}s"
				fi
			fi
		fi
	done <<< "$entries"

	# Heartbeat once per poll so silence is provably silence, not a dead
	# script. Lower-volume than per-event lines but still line-oriented.
	emit "HEARTBEAT plans=$n active=${active:-<none>}"
}

# Scan docker logs since last poll for wedge-shape signals. Emits one line
# per match. --since takes a duration so we use the saved epoch to compute.
#
# Two filters layered:
#   1. LEVEL prefilter — only INFO/WARN/ERROR semspec log lines reach the
#      wedge-shape grep. Pre-fix, DEBUG payload echoes (OpenAI API request
#      payload contains the agent's PROMPT, which has words like "reject"
#      and "exhausted") triggered hundreds of false-positive emits per
#      poll. Caught smoke 9 2026-06-02. Now DEBUG lines are dropped before
#      the wedge-shape regex even runs.
#   2. Timestamp dedup — log lines carry `time=<iso8601>` at column 0;
#      we track the highest emitted timestamp and drop any line at or
#      below it. Kills the cross-poll-boundary duplicate-emission shape
#      the +5s overlap window created in PR #86.
poll_docker_logs() {
	local last_epoch now_epoch since_seconds
	last_epoch="$(cat "$LAST_LOG_TS_FILE" 2>/dev/null || now_epoch)"
	now_epoch="$(now_epoch)"
	since_seconds=$((now_epoch - last_epoch + 5))  # +5s overlap to avoid gap
	[ "$since_seconds" -lt 10 ] && since_seconds=10

	local last_emit_ts
	last_emit_ts="$(cat "$LAST_EMIT_TS_FILE" 2>/dev/null || echo '')"

	# Filter is per-line and the output is forwarded verbatim. Truncate to
	# 240 chars per line so a runaway log doesn't blow up poll.log.
	#
	# LEVEL prefilter: keep only lines with level=(INFO|WARN|ERROR). The
	# alternation list is exhaustive — level=DEBUG simply isn't in the
	# set, so DEBUG lines are dropped. Case-insensitive (`-iE`) because
	# the downstream wedge-shape grep also accepts lowercase
	# `level=error` / `"level":"error"` — components or upstream
	# semstreams that emit lowercase levels must still flow through.
	# Silence is not success (per PR #86 design comment).
	local out
	out="$(docker logs --since="${since_seconds}s" "$CONTAINER" 2>&1 \
		| grep -iE 'level=(INFO|WARN|ERROR)|"level":"(INFO|WARN|ERROR)"' \
		| grep -iE '(reject|fixable_rejection|needs_changes|recovery.requested|recovery.complete|plan-decision|plan_decision|wedged|exhausted|terminal_failure|context deadline exceeded|level=error|"level":"error")' \
		| sed 's/[[:space:]]\+/ /g' \
		| cut -c1-240 \
		2>/dev/null || true)"

	local max_seen_ts="$last_emit_ts"

	if [ -n "$out" ]; then
		while IFS= read -r line; do
			[ -z "$line" ] && continue

			# Timestamp dedup: extract `time=<iso8601>` from the front of
			# the line; skip if at or before the last emitted timestamp.
			# ISO-8601 sorts lexicographically so plain bash [[ < ]] works.
			local line_ts
			line_ts="$(printf '%s' "$line" | grep -oE 'time=[^ ]+' | head -1 | sed 's/^time=//')"
			if [ -n "$line_ts" ] && [ -n "$last_emit_ts" ]; then
				# bash has no <= operator on strings, hence the negated >.
				if [[ ! "$line_ts" > "$last_emit_ts" ]]; then
					continue
				fi
			fi

			# Coarse classification — same line emits ONE category so we don't
			# double-count. Order matters: most specific first.
			if echo "$line" | grep -qiE 'recovery.requested|recovery.complete'; then
				emit "RECOVERY_DISPATCH line=\"$line\""
			elif echo "$line" | grep -qiE 'plan-decision|plan_decision'; then
				emit "PLAN_DECISION line=\"$line\""
			elif echo "$line" | grep -qiE 'reject|fixable_rejection|needs_changes'; then
				emit "REVIEWER_REJECT line=\"$line\""
			elif echo "$line" | grep -qiE 'wedged|exhausted|terminal_failure|context deadline exceeded'; then
				emit "WEDGE_SIGNAL line=\"$line\""
			else
				emit "ERROR line=\"$line\""
			fi

			# Track the highest log-line timestamp we've emitted this poll.
			if [ -n "$line_ts" ] && [[ "$line_ts" > "$max_seen_ts" ]]; then
				max_seen_ts="$line_ts"
			fi
		done <<< "$out"
	fi

	# Persist the high-water-mark for next poll's dedup.
	if [ -n "$max_seen_ts" ]; then
		printf '%s' "$max_seen_ts" > "$LAST_EMIT_TS_FILE"
	fi

	echo "$now_epoch" > "$LAST_LOG_TS_FILE"
}

# ── Main loop ────────────────────────────────────────────────────────────

while true; do
	poll_plan_stages
	poll_docker_logs
	sleep "$POLL"
done
