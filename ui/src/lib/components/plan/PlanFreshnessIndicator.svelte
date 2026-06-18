<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { planFreshnessIndicatorState } from '$lib/components/plan/observabilityModels';
	import { feedStore } from '$lib/stores/feed.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
	}

	let { plan }: Props = $props();

	const indicator = $derived(planFreshnessIndicatorState(plan, {
		currentSlug: feedStore.currentSlug,
		connected: feedStore.connected,
		streamEverConnected: feedStore.streamEverConnected,
		lastSuccessfulUpdateAt: feedStore.lastSuccessfulUpdateAt,
		questionsConnected: questionsStore.connected,
		questionsEverConnected: questionsStore.streamEverConnected,
		questionsLastSuccessfulUpdateAt: questionsStore.lastSuccessfulUpdateAt,
		questionsError: questionsStore.streamError,
		questionsLastErrorAt: questionsStore.lastErrorAt
	}));

	function formatTime(timestamp: string): string {
		return new Date(timestamp).toLocaleString();
	}
</script>

{#if indicator.shouldShow}
	<div class="freshness-indicator" data-state={indicator.disconnected ? 'disconnected' : 'stale'} role="status">
		<Icon name={indicator.disconnected ? 'wifi-off' : 'clock'} size={16} />
		<div class="freshness-copy">
			<strong>{indicator.statusLabel}</strong>
			<div class="freshness-meta">
				{#if indicator.lastUpdateAt}
					<span>Last successful update {formatTime(indicator.lastUpdateAt)}</span>
				{/if}
				{#if indicator.reason}
					<span>{indicator.reason}</span>
				{/if}
				{#if indicator.source}
					<span>{indicator.source}</span>
				{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.freshness-indicator {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-3);
		border: 1px solid var(--color-warning-muted, rgba(245, 158, 11, 0.3));
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		color: var(--color-text-secondary);
	}

	.freshness-indicator[data-state='disconnected'] {
		border-color: var(--color-error-muted, rgba(239, 68, 68, 0.28));
	}

	.freshness-copy {
		display: flex;
		min-width: 0;
		flex-direction: column;
		gap: var(--space-1);
		font-size: var(--font-size-sm);
	}

	.freshness-copy strong {
		color: var(--color-text-primary);
		font-weight: var(--font-weight-semibold);
	}

	.freshness-meta {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.freshness-meta span {
		overflow-wrap: anywhere;
	}
</style>
