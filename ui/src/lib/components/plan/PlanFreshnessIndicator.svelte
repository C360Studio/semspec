<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
	}

	let { plan }: Props = $props();

	const freshness = $derived(plan.phase_summary?.freshness ?? null);
	const planScoped = $derived(feedStore.currentSlug === plan.slug);
	const disconnected = $derived(
		planScoped && feedStore.streamEverConnected && !feedStore.connected
	);
	const stale = $derived(Boolean(freshness?.stale));
	const shouldShow = $derived(stale || disconnected);
	const lastUpdateAt = $derived(
		feedStore.lastSuccessfulUpdateAt ?? freshness?.generated_at ?? null
	);
	const statusLabel = $derived.by(() => {
		if (stale && disconnected) return 'Stale data and stream disconnected';
		if (stale) return 'Stale data';
		return 'Stream disconnected';
	});

	function formatTime(timestamp: string): string {
		return new Date(timestamp).toLocaleString();
	}
</script>

{#if shouldShow}
	<div class="freshness-indicator" data-state={disconnected ? 'disconnected' : 'stale'} role="status">
		<Icon name={disconnected ? 'wifi-off' : 'clock'} size={16} />
		<div class="freshness-copy">
			<strong>{statusLabel}</strong>
			<div class="freshness-meta">
				{#if lastUpdateAt}
					<span>Last successful update {formatTime(lastUpdateAt)}</span>
				{/if}
				{#if freshness?.reason}
					<span>{freshness.reason}</span>
				{/if}
				{#if freshness?.source}
					<span>{freshness.source}</span>
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
