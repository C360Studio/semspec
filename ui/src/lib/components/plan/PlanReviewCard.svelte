<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';

	interface Props {
		verdict: string;
		summary?: string | null;
		reviewedAt?: string | null;
		iteration?: number | null;
	}

	let { verdict, summary, reviewedAt, iteration }: Props = $props();

	const verdictClass = $derived(
		verdict === 'approved' ? 'success' : verdict === 'needs_changes' ? 'warning' : 'neutral'
	);

	const verdictLabel = $derived(
		verdict === 'approved'
			? 'Approved'
			: verdict === 'needs_changes'
				? 'Needs Changes'
				: verdict
	);

	const reviewedAtFormatted = $derived(
		reviewedAt ? new Date(reviewedAt).toLocaleString() : null
	);
</script>

<section class="plan-review-card">
	<header class="card-header">
		<div class="card-title">
			<Icon name="user-check" size={14} />
			<span>Plan Reviewer</span>
		</div>
		<div class="card-meta">
			{#if iteration && iteration > 1}
				<span class="iteration-chip" title="Number of review rounds">
					Round {iteration}
				</span>
			{/if}
			<span class="verdict-chip verdict-{verdictClass}">{verdictLabel}</span>
		</div>
	</header>

	{#if summary}
		<p class="card-summary">{summary}</p>
	{/if}

	{#if reviewedAtFormatted}
		<footer class="card-footer">
			<Icon name="clock" size={12} />
			<span>{reviewedAtFormatted}</span>
		</footer>
	{/if}
</section>

<style>
	.plan-review-card {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.card-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.card-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.iteration-chip {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
	}

	.verdict-chip {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-3);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-full);
	}

	.verdict-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.verdict-warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.verdict-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	.card-summary {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-normal);
		color: var(--color-text-primary);
	}

	.card-footer {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
