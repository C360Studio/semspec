<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { projectActivityFeed } from '$lib/stores/activityProjection';
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

	const sourceOptions = ['all', 'plan', 'execution', 'question'];

	const filteredEvents = $derived.by(() => {
		let events: FeedEvent[] =
			scope === 'global'
				? projectActivityFeed(activityStore.recent, maxEvents)
				: feedStore.events.slice(-maxEvents);

		if (sourceFilter !== 'all') {
			events = events.filter((e) => e.source === sourceFilter);
		}

		return events;
	});

	// Connection indicator reflects which scope's SSE we're rendering.
	const isConnected = $derived(
		scope === 'global' ? activityStore.connected : feedStore.connected
	);
	const waitingLabel = $derived(
		scope === 'global' ? 'Activity stream offline' : 'Waiting for plan...'
	);

	function getEventIcon(event: FeedEvent): string {
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
			case 'question':
				if (event.type === 'question_answered') return 'check-circle';
				if (event.type === 'question_timeout') return 'clock';
				return 'help-circle';
			default:
				return 'activity';
		}
	}

	function getEventColor(event: FeedEvent): string {
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
			question: 'Questions'
		};
		return labels[source] ?? source;
	}

	function getEventHref(event: FeedEvent): string | null {
		const loopId = event.data?.loop_id;
		if (
			typeof loopId === 'string' &&
			loopId.length > 0 &&
			(event.type === 'task_updated' || event.type === 'task_completed')
		) {
			return `/trajectories/${loopId}`;
		}
		if (event.slug) {
			return `/plans/${event.slug}`;
		}
		return null;
	}

	function getEventLinkText(event: FeedEvent): string {
		const loopId = event.data?.loop_id;
		if (
			typeof loopId === 'string' &&
			loopId.length > 0 &&
			(event.type === 'task_updated' || event.type === 'task_completed')
		) {
			return 'trajectory';
		}
		return event.slug ?? 'plan';
	}
</script>

<div class="activity-feed">
	<div class="feed-header">
		<h2 class="feed-title">Activity Feed</h2>
		<div class="feed-filters">
			<select
				bind:value={sourceFilter}
				class="filter-select"
				aria-label="Filter by event source"
			>
				{#each sourceOptions as source}
					<option value={source}>{sourceLabel(source)}</option>
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
		<div class="events-list" role="log" aria-live="polite">
			{#each filteredEvents as event (event.id)}
				<div class="event-item">
					<div class="event-icon" style="color: {getEventColor(event)}">
						<Icon name={getEventIcon(event)} size={14} />
					</div>

					<div class="event-body">
						<div class="event-summary">
							<span class="event-text">{event.summary}</span>
							<span class="event-time">{formatTime(event.timestamp)}</span>
						</div>

						<div class="event-meta">
							<span class="event-source-tag {event.source}">{event.source}</span>
							{#if getEventHref(event)}
								<a href={getEventHref(event)} class="event-plan-tag">{getEventLinkText(event)}</a>
							{/if}
						</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.activity-feed {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
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

	.event-item:hover {
		background: var(--color-bg-tertiary);
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

	.event-source-tag.question {
		background: color-mix(in srgb, var(--color-warning, var(--color-error)) 15%, transparent);
		color: var(--color-warning, var(--color-error));
	}

	.event-plan-tag {
		font-size: 10px;
		padding: 1px 4px;
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		text-decoration: none;
		font-family: var(--font-family-mono);
	}

	.event-plan-tag:hover {
		text-decoration: none;
		background: var(--color-accent);
		color: white;
	}
</style>
