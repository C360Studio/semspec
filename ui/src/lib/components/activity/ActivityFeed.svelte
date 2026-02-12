<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { changesStore } from '$lib/stores/changes.svelte';
	import type { ActivityEvent } from '$lib/types';

	interface Props {
		maxEvents?: number;
		changeFilter?: string;
	}

	let { maxEvents = 50, changeFilter }: Props = $props();

	let typeFilter = $state<string>('all');

	const eventTypes = [
		'all',
		'loop_created',
		'loop_updated',
		'loop_deleted',
		'tool_call',
		'model_call'
	];

	const filteredEvents = $derived.by(() => {
		let events = activityStore.recent.slice(0, maxEvents);

		if (typeFilter !== 'all') {
			events = events.filter((e) => e.type === typeFilter);
		}

		// TODO: Filter by change slug when backend adds workflow_slug to events
		// For now, we can try to match loop_id to active loops in changes
		if (changeFilter) {
			const change = changesStore.getBySlug(changeFilter);
			if (change) {
				const loopIds = change.active_loops.map((l) => l.loop_id);
				events = events.filter((e) => loopIds.includes(e.loop_id));
			}
		}

		return events;
	});

	function getEventIcon(type: string): string {
		switch (type) {
			case 'loop_created':
				return 'play';
			case 'loop_updated':
				return 'activity';
			case 'loop_deleted':
				return 'check';
			case 'tool_call':
				return 'wrench';
			case 'model_call':
				return 'brain';
			default:
				return 'circle';
		}
	}

	function getEventColor(type: string): string {
		switch (type) {
			case 'loop_created':
				return 'var(--color-success)';
			case 'loop_deleted':
				return 'var(--color-success)';
			case 'tool_call':
				return 'var(--color-accent)';
			case 'model_call':
				return 'var(--color-info)';
			default:
				return 'var(--color-text-muted)';
		}
	}

	function formatEventType(type: string): string {
		return type.replace(/_/g, ' ');
	}

	function formatTime(timestamp: string): string {
		return new Date(timestamp).toLocaleTimeString();
	}

	function parseEventData(event: ActivityEvent): Record<string, unknown> | null {
		if (!event.data) return null;
		try {
			return JSON.parse(event.data);
		} catch {
			return null;
		}
	}

	// Find which change a loop belongs to
	function getChangeSlugForLoop(loopId: string): string | undefined {
		for (const change of changesStore.all) {
			if (change.active_loops.some((l) => l.loop_id === loopId)) {
				return change.slug;
			}
		}
		return undefined;
	}
</script>

<div class="activity-feed">
	<div class="feed-header">
		<h2 class="feed-title">Activity Feed</h2>
		<div class="feed-filters">
			<select
				bind:value={typeFilter}
				class="filter-select"
				aria-label="Filter by event type"
			>
				{#each eventTypes as type}
					<option value={type}>{type === 'all' ? 'All events' : formatEventType(type)}</option>
				{/each}
			</select>
		</div>
	</div>

	<div class="feed-status">
		<div class="connection-indicator" class:connected={activityStore.connected}>
			<span class="status-dot"></span>
			<span>{activityStore.connected ? 'Live' : 'Connecting...'}</span>
		</div>
		<span class="event-count">{filteredEvents.length} events</span>
	</div>

	{#if filteredEvents.length === 0}
		<div class="empty-feed">
			<Icon name="activity" size={32} />
			<p>No activity yet</p>
			<p class="hint">Events will appear here as agents work</p>
		</div>
	{:else}
		<div class="events-list" role="log" aria-live="polite">
			{#each filteredEvents as event (event.timestamp + event.loop_id)}
				{@const data = parseEventData(event)}
				{@const changeSlug = getChangeSlugForLoop(event.loop_id)}
				<div class="event-item">
					<div class="event-icon" style="color: {getEventColor(event.type)}">
						<Icon name={getEventIcon(event.type)} size={14} />
					</div>

					<div class="event-body">
						<div class="event-header">
							<span class="event-time">{formatTime(event.timestamp)}</span>
							{#if changeSlug}
								<a href="/changes/{changeSlug}" class="event-change-tag">
									{changeSlug}
								</a>
							{/if}
						</div>

						<div class="event-content">
							<span class="event-type">{formatEventType(event.type)}</span>
							<span class="event-loop">
								<Icon name="activity" size={10} />
								{event.loop_id.slice(-6)}
							</span>
						</div>

						{#if data}
							<div class="event-data">
								{#if data.state}
									<span class="data-badge">state: {data.state}</span>
								{/if}
								{#if data.iterations !== undefined}
									<span class="data-badge">iter: {data.iterations}</span>
								{/if}
								{#if data.tool}
									<span class="data-badge">tool: {data.tool}</span>
								{/if}
								{#if data.model}
									<span class="data-badge">model: {data.model}</span>
								{/if}
							</div>
						{/if}
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

	.event-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-1);
	}

	.event-time {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-variant-numeric: tabular-nums;
	}

	.event-change-tag {
		font-size: var(--font-size-xs);
		padding: 1px 6px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-sm);
		text-decoration: none;
	}

	.event-change-tag:hover {
		text-decoration: none;
		background: var(--color-accent);
		color: white;
	}

	.event-content {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.event-type {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		text-transform: capitalize;
	}

	.event-loop {
		display: flex;
		align-items: center;
		gap: 2px;
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.event-data {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		margin-top: var(--space-1);
	}

	.data-badge {
		font-size: 10px;
		padding: 1px 4px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}
</style>
