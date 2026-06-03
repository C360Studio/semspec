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
# Env: POLL_INTERVAL (default 30s), WEDGE_WARN_SECONDS (default 600).

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

# Track when we last scraped docker logs so we never re-emit a line.
LAST_LOG_TS_FILE="$OUT_DIR/.poll-state/last-log-epoch"
echo "$(now_epoch)" > "$LAST_LOG_TS_FILE"

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
			if [ "$elapsed" -gt "$WEDGE_WARN" ]; then
				# Emit once per WEDGE_WARN window to avoid log spam.
				local window=$((elapsed / WEDGE_WARN))
				local marker="$OUT_DIR/.poll-state/wedge-$slug-$stage-$window"
				if [ ! -f "$marker" ]; then
					touch "$marker"
					emit "WEDGE_DETECT slug=$slug stage=$stage stuck_for=${elapsed}s"
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
poll_docker_logs() {
	local last_epoch now_epoch since_seconds
	last_epoch="$(cat "$LAST_LOG_TS_FILE" 2>/dev/null || now_epoch)"
	now_epoch="$(now_epoch)"
	since_seconds=$((now_epoch - last_epoch + 5))  # +5s overlap to avoid gap
	[ "$since_seconds" -lt 10 ] && since_seconds=10

	# `docker logs --since=<seconds>s` is the post-25.0 syntax. The grep
	# alternation MUST stay broad — silence is not success. We're hunting
	# for any of: reviewer rejection, recovery emit, plan-decision lifecycle,
	# explicit error/failure markers.
	#
	# Filter is per-line and the output is forwarded verbatim. Truncate to
	# 240 chars per line so a runaway log doesn't blow up poll.log.
	local out
	out="$(docker logs --since="${since_seconds}s" "$CONTAINER" 2>&1 \
		| grep -iE '(reject|fixable_rejection|needs_changes|recovery.requested|recovery.complete|plan-decision|plan_decision|wedged|exhausted|terminal_failure|context deadline exceeded|level=error|"level":"error")' \
		| sed 's/[[:space:]]\+/ /g' \
		| cut -c1-240 \
		2>/dev/null || true)"

	if [ -n "$out" ]; then
		while IFS= read -r line; do
			[ -z "$line" ] && continue
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
		done <<< "$out"
	fi

	echo "$now_epoch" > "$LAST_LOG_TS_FILE"
}

# ── Main loop ────────────────────────────────────────────────────────────

while true; do
	poll_plan_stages
	poll_docker_logs
	sleep "$POLL"
done
