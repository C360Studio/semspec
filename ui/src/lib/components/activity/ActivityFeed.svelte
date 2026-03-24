<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { ActivityEvent } from '$lib/types';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plans?: PlanWithStatus[];
		maxEvents?: number;
		planFilter?: string;
	}

	let { plans = [], maxEvents = 50, planFilter }: Props = $props();

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

		// TODO: Filter by plan slug when backend adds plan_slug to events
		// For now, we can try to match loop_id to active loops in plans
		if (planFilter) {
			const plan = plans.find((p) => p.slug === planFilter);
			if (plan) {
				const loopIds = (plan.active_loops ?? []).map((l) => l.loop_id);
				events = events.filter((e) => loopIds.includes(e.loop_id));
			}
		}

		return events;
	});

	function getEventIcon(type: string, data: Record<string, unknown> | null): string {
		if (data?.tool) return 'terminal';
		if (data?.step === 'build' || data?.step === 'builder') return 'hammer';
		if (data?.step === 'test' || data?.step === 'tester') return 'test-tube';
		if (data?.step === 'review' || data?.step === 'reviewer') return 'check-square';
		if (data?.step === 'decompose') return 'scissors';
		switch (type) {
			case 'loop_created':
				return 'play';
			case 'loop_deleted':
				return 'check-circle';
			case 'model_call':
				return 'brain';
			default:
				return 'activity';
		}
	}

	function getEventColor(type: string, data: Record<string, unknown> | null): string {
		if (data?.state === 'success' || type === 'loop_deleted') return 'var(--color-success)';
		if (data?.state === 'failed' || data?.state === 'error') return 'var(--color-error)';
		if (type === 'loop_created') return 'var(--color-success)';
		if (data?.tool) return 'var(--color-accent)';
		return 'var(--color-text-muted)';
	}

	/**
	 * Build a human-readable summary from event type + data.
	 * e.g. "Builder writing code", "Tester running tests", "bash: go test ./..."
	 */
	function formatEventSummary(type: string, data: Record<string, unknown> | null): string {
		if (data?.tool) {
			const tool = String(data.tool);
			const args = data.args ? String(data.args).slice(0, 60) : '';
			return args ? `${tool}: ${args}` : tool;
		}

		const step = String(data?.step ?? data?.workflow_step ?? '');
		const state = String(data?.state ?? '');

		if (type === 'loop_created') {
			return step ? `${formatStep(step)} started` : 'Agent started';
		}
		if (type === 'loop_deleted' || state === 'success') {
			return step ? `${formatStep(step)} completed` : 'Agent completed';
		}
		if (state === 'failed' || state === 'error') {
			return step ? `${formatStep(step)} failed` : 'Agent failed';
		}
		if (state === 'executing') {
			return step ? `${formatStep(step)} working...` : 'Agent working...';
		}

		// Fallback
		if (step) return `${formatStep(step)} updated`;
		return type.replace(/_/g, ' ');
	}

	function formatStep(step: string): string {
		const labels: Record<string, string> = {
			decompose: 'Decomposer',
			build: 'Builder',
			builder: 'Builder',
			test: 'Tester',
			tester: 'Tester',
			validate: 'Validator',
			validator: 'Validator',
			review: 'Reviewer',
			reviewer: 'Reviewer',
			plan: 'Planner',
			planner: 'Planner'
		};
		return labels[step] ?? step.charAt(0).toUpperCase() + step.slice(1);
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

	// Find which plan a loop belongs to
	function getPlanSlugForLoop(loopId: string): string | undefined {
		for (const plan of plans) {
			if (plan.active_loops?.some((l) => l.loop_id === loopId)) {
				return plan.slug;
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
					<option value={type}>{type === 'all' ? 'All events' : type.replace(/_/g, ' ')}</option>
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
				{@const planSlug = getPlanSlugForLoop(event.loop_id)}
				<div class="event-item">
					<div class="event-icon" style="color: {getEventColor(event.type, data)}">
						<Icon name={getEventIcon(event.type, data)} size={14} />
					</div>

					<div class="event-body">
						<div class="event-summary">
							<span class="event-text">{formatEventSummary(event.type, data)}</span>
							<span class="event-time">{formatTime(event.timestamp)}</span>
						</div>

						<div class="event-meta">
							{#if planSlug}
								<a href="/plans/{planSlug}" class="event-plan-tag">{planSlug}</a>
							{/if}
							<span class="event-loop-id">{event.loop_id.slice(-6)}</span>
							{#if data?.iterations !== undefined}
								<span class="event-iter">iter {data.iterations}</span>
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

	.event-plan-tag {
		font-size: 10px;
		padding: 1px 4px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-sm);
		text-decoration: none;
	}

	.event-loop-id {
		font-family: var(--font-family-mono);
		font-size: 10px;
		color: var(--color-text-muted);
	}

	.event-iter {
		font-size: 10px;
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	.event-plan-tag:hover {
		text-decoration: none;
		background: var(--color-accent);
		color: white;
	}

</style>
