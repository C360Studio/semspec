<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { projectActivityFeed } from '$lib/stores/activityProjection';
	import {
		getEventHref,
		getEventLinkText,
		getRequirementAnchor,
		countBySource
	} from './feedRouting';
	import type { FeedEvent } from '$lib/types/feed';

	type Scope = 'plan' | 'global';

	interface Props {
		maxEvents?: number;
		/**
		 * 'plan' (default) renders feedStore.events — plan-scoped SSE populated
		 * by plan detail pages. 'global' renders activityStore.recent projected
		 * into FeedEvent shape — the only source available on /board where no
		 * plan is selected. Fixes bug #7.2: ActivityFeed on /board used to be
		 * permanently empty because feedStore is plan-scoped only.
		 */
		scope?: Scope;
	}

	let { maxEvents = 100, scope = 'plan' }: Props = $props();

	let sourceFilter = $state<string>('all');

	const sourceOptions = ['all', 'plan', 'execution', 'activity', 'question'];
	const sourcesForCount = ['plan', 'execution', 'activity', 'question'] as const;

	// All events before the source-filter is applied. We need the unfiltered
	// list so per-source counts in the dropdown reflect the totals, not the
	// current filter's narrowed view (bug #7.5).
	const allEvents = $derived.by(() =>
		scope === 'global'
			? projectActivityFeed(activityStore.recent, maxEvents)
			: feedStore.events.slice(-maxEvents)
	);

	const sourceCounts = $derived(countBySource(allEvents, sourcesForCount));

	const filteredEvents = $derived.by(() => {
		if (sourceFilter === 'all') return allEvents;
		return allEvents.filter((e) => e.source === sourceFilter);
	});

	// Connection indicator reflects which scope's SSE we're rendering.
	const isConnected = $derived(
		scope === 'global' ? activityStore.connected : feedStore.connected
	);
	const waitingLabel = $derived(
		scope === 'global' ? 'Activity stream offline' : 'Waiting for plan...'
	);

	function getEventIcon(event: FeedEvent): string {
		if (event.kind === 'plan_recovery') return 'refresh-cw';
		if (event.kind === 'plan_wait') return 'clock';
		if (event.kind === 'plan_stale' || event.kind === 'execution_stale') return 'wifi-off';
		if (event.kind === 'execution_orphaned') return 'unlink';
		if (event.kind === 'lesson_activity') return 'lightbulb';
		switch (event.source) {
			case 'plan':
				if (event.type === 'plan_deleted') return 'trash-2';
				return 'git-pull-request';
			case 'execution':
				if (event.type.startsWith('task')) {
					const stage = (event.data?.stage as string) ?? '';
					if (stage === 'testing') return 'test-tube';
					if (stage === 'building') return 'hammer';
					if (stage === 'validating') return 'check-square';
					if (stage === 'reviewing') return 'eye';
					if (stage === 'approved') return 'check-circle';
					if (stage === 'escalated' || stage === 'error') return 'alert-triangle';
					return 'hammer';
				}
				return 'layers';
			case 'activity':
				return 'activity';
			case 'question':
				if (event.type === 'question_answered') return 'check-circle';
				if (event.type === 'question_timeout') return 'clock';
				return 'help-circle';
			default:
				return 'activity';
		}
	}

	function getEventColor(event: FeedEvent): string {
		if (event.kind === 'plan_recovery') return 'var(--color-warning, var(--color-accent))';
		if (event.kind === 'plan_wait') return 'var(--color-warning, var(--color-text-muted))';
		if (event.kind === 'plan_stale' || event.kind === 'execution_stale' || event.kind === 'execution_orphaned') {
			return 'var(--color-warning, var(--color-error))';
		}
		if (event.kind === 'lesson_activity') return 'var(--color-accent)';
		switch (event.source) {
			case 'plan': {
				const stage = (event.data?.stage as string) ?? '';
				if (stage === 'complete') return 'var(--color-success)';
				if (stage === 'failed') return 'var(--color-error)';
				return 'var(--color-accent)';
			}
			case 'execution': {
				const stage = (event.data?.stage as string) ?? '';
				if (stage === 'approved' || stage === 'completed') return 'var(--color-success)';
				if (stage === 'error' || stage === 'failed' || stage === 'escalated') return 'var(--color-error)';
				return 'var(--color-text-muted)';
			}
			case 'activity':
				return 'var(--color-text-muted)';
			case 'question':
				if (event.type === 'question_answered') return 'var(--color-success)';
				if (event.type === 'question_timeout') return 'var(--color-warning, var(--color-error))';
				return 'var(--color-accent)';
			default:
				return 'var(--color-text-muted)';
		}
	}

	function formatTime(timestamp: string): string {
		return new Date(timestamp).toLocaleTimeString();
	}

	function sourceLabel(source: string): string {
		const labels: Record<string, string> = {
			all: 'All events',
			plan: 'Plan',
			execution: 'Execution',
			activity: 'Activity',
			question: 'Questions'
		};
		return labels[source] ?? source;
	}

	/**
	 * Build the dropdown option text with a per-source count appended.
	 * Bug #7.5: without counts users can't tell which source dominates
	 * without scrolling through the list; the count makes "most events are
	 * execution ticks" legible from the collapsed dropdown state.
	 */
	function sourceOptionText(source: string): string {
		const count = sourceCounts[source] ?? 0;
		return `${sourceLabel(source)} (${count})`;
	}

	// -----------------------------------------------------------------------
	// Autoscroll-to-newest (chat-app pattern)
	// -----------------------------------------------------------------------
	// Events are appended at the bottom. When the user is reading the latest
	// activity (scrolled to the bottom), new events should auto-scroll into
	// view. When the user has scrolled up to read history, we MUST NOT yank
	// their scroll position — instead surface a "N new ↓" pill so they can
	// opt back into following the live tail.
	//
	// Design discipline: `isUserPinnedToBottom` is set ONLY from the user's
	// scroll handler (user event → state update is fine). `lastSeenIndex` is
	// set ONLY when the user explicitly jumps to bottom (pill click or
	// scrolls there manually). The autoscroll itself is a $effect that does
	// nothing but mutate the DOM scrollTop — no reactive state assignment
	// inside the effect (avoids the abuse pattern caught 2026-05-19, see
	// [[svelte5-effect-for-side-effects-only]]).
	let listEl = $state<HTMLDivElement | null>(null);
	let isUserPinnedToBottom = $state(true);
	let lastSeenIndex = $state(0);

	// Pixels from absolute bottom that still count as "at the bottom".
	// Generous threshold absorbs sub-pixel rounding and the in-flight
	// growth of the row that triggered the scroll event.
	const PINNED_THRESHOLD_PX = 48;

	const newEventsBelow = $derived(
		Math.max(0, filteredEvents.length - lastSeenIndex)
	);

	function handleScroll() {
		if (!listEl) return;
		const distanceFromBottom =
			listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight;
		const nowPinned = distanceFromBottom <= PINNED_THRESHOLD_PX;
		isUserPinnedToBottom = nowPinned;
		if (nowPinned) {
			lastSeenIndex = filteredEvents.length;
		}
	}

	function jumpToNewest() {
		if (!listEl) return;
		listEl.scrollTop = listEl.scrollHeight;
		isUserPinnedToBottom = true;
		lastSeenIndex = filteredEvents.length;
	}

	// Side-effect-only $effect: when filteredEvents grows AND the user is
	// pinned to the bottom, follow the new content. Reads reactive state
	// (length, pinned) and mutates the DOM — no reactive state assignment
	// inside.
	$effect(() => {
		const len = filteredEvents.length;
		if (!listEl) return;
		if (!isUserPinnedToBottom) return;
		// Read length so this effect is properly tracked against new events,
		// then schedule scroll after the DOM has appended the new row.
		void len;
		queueMicrotask(() => {
			if (!listEl) return;
			listEl.scrollTop = listEl.scrollHeight;
		});
	});
</script>

<div class="activity-feed">
	<div class="feed-header">
		<h2 class="feed-title">Activity Feed</h2>
		<div class="feed-filters">
			<select
				bind:value={sourceFilter}
				class="filter-select"
				aria-label="Filter by event source"
				data-testid="feed-source-filter"
			>
				{#each sourceOptions as source}
					<option
						value={source}
						data-testid="feed-source-option"
						data-source={source}
						data-count={sourceCounts[source] ?? 0}>{sourceOptionText(source)}</option>
				{/each}
			</select>
		</div>
	</div>

	<div class="feed-status">
		<div class="connection-indicator" class:connected={isConnected}>
			<span class="status-dot"></span>
			<span>{isConnected ? 'Live' : waitingLabel}</span>
		</div>
		<span class="event-count">{filteredEvents.length} events</span>
	</div>

	{#if filteredEvents.length === 0}
		<div class="empty-feed">
			<Icon name="activity" size={32} />
			{#if isConnected}
				<p>No activity yet</p>
				<p class="hint">
					{scope === 'global'
						? 'Loop events will appear as agents start'
						: 'Events will appear as the plan progresses'}
				</p>
			{:else if scope === 'global'}
				<p>Activity stream offline</p>
				<p class="hint">Global loop events will appear when the stream reconnects</p>
			{:else}
				<p>Select a plan to see activity</p>
				<p class="hint">Plan stages, execution progress, and questions will appear here</p>
			{/if}
		</div>
	{:else}
		<div
			class="events-list"
			role="log"
			bind:this={listEl}
			onscroll={handleScroll}
		>
			{#snippet anchorPill(anchor: string)}
				<span
					class="req-anchor"
					data-testid="req-anchor"
					aria-label="Requirement {anchor}"
					title="Requirement {anchor}">{anchor}</span>
			{/snippet}
			{#each filteredEvents as event (event.id)}
				{@const href = getEventHref(event)}
				{@const reqAnchor = getRequirementAnchor(event)}
				{#if href}
					<a
						class="event-item event-item--link"
						href={href}
						data-testid="activity-feed-row"
						data-kind={event.kind}
						data-href={href}
					>
						<div class="event-icon" style="color: {getEventColor(event)}">
							<Icon name={getEventIcon(event)} size={14} />
						</div>
						<div class="event-body">
							<div class="event-summary">
								{#if reqAnchor}{@render anchorPill(reqAnchor)}{/if}
								<span class="event-text">{event.summary}</span>
								<span class="event-time">{formatTime(event.timestamp)}</span>
							</div>
							<div class="event-meta">
								<span class="event-source-tag {event.source}">{event.source}</span>
								<span class="event-plan-tag">{getEventLinkText(event)}</span>
							</div>
						</div>
					</a>
				{:else}
					<div class="event-item" data-testid="activity-feed-row" data-kind={event.kind}>
						<div class="event-icon" style="color: {getEventColor(event)}">
							<Icon name={getEventIcon(event)} size={14} />
						</div>
						<div class="event-body">
							<div class="event-summary">
								{#if reqAnchor}{@render anchorPill(reqAnchor)}{/if}
								<span class="event-text">{event.summary}</span>
								<span class="event-time">{formatTime(event.timestamp)}</span>
							</div>
							<div class="event-meta">
								<span class="event-source-tag {event.source}">{event.source}</span>
								<!-- Spacer badge keeps non-link rows the same height as link rows
								     so the list doesn't shift visually when an unlinkable event
								     lands between linkable ones. -->
								<span class="event-plan-tag event-plan-tag--muted">&mdash;</span>
							</div>
						</div>
					</div>
				{/if}
			{/each}
		</div>
		{#if newEventsBelow > 0 && !isUserPinnedToBottom}
			<button
				type="button"
				class="new-events-pill"
				onclick={jumpToNewest}
				data-testid="new-events-pill"
				aria-label="Jump to {newEventsBelow} new event{newEventsBelow === 1 ? '' : 's'}"
			>
				<Icon name="arrow-down" size={12} />
				<span>{newEventsBelow} new</span>
			</button>
		{/if}
	{/if}
</div>

<style>
	.activity-feed {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
		/* Anchor for the absolutely-positioned `.new-events-pill` that floats
		 * over the bottom of the events list when the user has scrolled up
		 * and new activity is below the viewport. */
		position: relative;
	}

	.new-events-pill {
		position: absolute;
		bottom: var(--space-4);
		right: var(--space-4);
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
		transition: transform var(--transition-fast), box-shadow var(--transition-fast);
		z-index: 1;
	}

	.new-events-pill:hover {
		transform: translateY(-1px);
		box-shadow: 0 6px 14px rgba(0, 0, 0, 0.3);
	}

	.new-events-pill:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.feed-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding-bottom: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		margin-bottom: var(--space-3);
	}

	.feed-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.filter-select {
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-xs);
		/* Prevents header jitter as counts grow from "(0)" to "(999)"; anchored
		 * at "All events (999)" which is ~16ch. Bug #7.5. */
		min-width: 14ch;
	}

	.feed-status {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-3);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.connection-indicator {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.connection-indicator.connected .status-dot {
		background: var(--color-success);
	}

	.empty-feed {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		color: var(--color-text-muted);
		gap: var(--space-2);
		text-align: center;
	}

	.empty-feed p {
		margin: 0;
	}

	.hint {
		font-size: var(--font-size-xs);
	}

	.events-list {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.event-item {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		transition: background var(--transition-fast);
	}

	/* Linkified rows — the whole row navigates. Keeps text color neutral
	 * (not link-blue) so the row looks like a clickable card rather than a
	 * conventional underlined link. Focus ring for keyboard operability. */
	.event-item--link {
		color: inherit;
		text-decoration: none;
		cursor: pointer;
	}

	.event-item--link:hover {
		background: var(--color-bg-tertiary);
	}

	.event-item--link:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 1px;
	}

	.event-icon {
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		flex-shrink: 0;
	}

	.event-body {
		flex: 1;
		min-width: 0;
	}

	.event-summary {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}

	/* Requirement anchor pill — visual hook so the eye can filter rows by
	 * requirement without reading every summary (bug #7.9). */
	.req-anchor {
		flex-shrink: 0;
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		padding: 1px 5px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-sm);
		font-family: var(--font-family-mono);
		letter-spacing: 0.02em;
	}

	.event-text {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.event-time {
		font-size: 10px;
		color: var(--color-text-muted);
		font-variant-numeric: tabular-nums;
		flex-shrink: 0;
	}

	.event-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: 2px;
	}

	.event-source-tag {
		font-size: 10px;
		padding: 1px 4px;
		border-radius: var(--radius-sm);
		text-transform: uppercase;
		letter-spacing: 0.03em;
	}

	.event-source-tag.plan {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.event-source-tag.execution {
		background: color-mix(in srgb, var(--color-success) 15%, transparent);
		color: var(--color-success);
	}

	.event-source-tag.activity {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.event-source-tag.question {
		background: color-mix(in srgb, var(--color-warning, var(--color-error)) 15%, transparent);
		color: var(--color-warning, var(--color-error));
	}

	/* Badge now; the whole row is the click target (bug #7.8) so this is
	 * just a visual indicator of the destination, not an interactive element. */
	.event-plan-tag {
		font-size: 10px;
		padding: 1px 4px;
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		font-family: var(--font-family-mono);
	}

	/* Spacer variant for non-link rows — matches dimensions without visual weight. */
	.event-plan-tag--muted {
		opacity: 0.35;
		background: transparent;
	}
</style>
