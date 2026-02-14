<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { ReviewerSummary, SpecVerdict } from '$lib/types/review';
	import { getVerdictLabel, getVerdictClass } from '$lib/types/review';

	interface Props {
		/** Spec reviewer summary from synthesis result */
		reviewer: ReviewerSummary | undefined;
		/** Whether the gate is loading */
		loading?: boolean;
	}

	let { reviewer, loading = false }: Props = $props();

	const verdict = $derived((reviewer?.verdict as SpecVerdict) || 'compliant');
	const passed = $derived(reviewer?.passed ?? false);
	const verdictLabel = $derived(getVerdictLabel(verdict));
	const verdictClass = $derived(getVerdictClass(verdict));
</script>

<div class="spec-gate" class:loading class:passed class:failed={!passed && !loading}>
	<div class="gate-header">
		<div class="gate-title">
			<Icon name={passed ? 'check-circle' : 'alert-circle'} size={18} />
			<span>Stage 1: Spec Compliance</span>
		</div>
		{#if !loading && reviewer}
			<span class="badge badge-{verdictClass}">
				{verdictLabel}
			</span>
		{/if}
	</div>

	{#if loading}
		<div class="gate-status loading">
			<Icon name="loader" size={16} class="spin" />
			<span>Checking spec compliance...</span>
		</div>
	{:else if reviewer}
		<div class="gate-status" class:success={passed} class:error={!passed}>
			{#if passed}
				<Icon name="check" size={16} />
				<span>Gate Passed</span>
			{:else}
				<Icon name="x" size={16} />
				<span>Gate Failed</span>
			{/if}
		</div>

		{#if reviewer.summary}
			<p class="gate-summary">{reviewer.summary}</p>
		{/if}
	{:else}
		<div class="gate-status neutral">
			<Icon name="clock" size={16} />
			<span>Awaiting review</span>
		</div>
	{/if}
</div>

<style>
	.spec-gate {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-4);
	}

	.spec-gate.passed {
		border-color: var(--color-success);
	}

	.spec-gate.failed {
		border-color: var(--color-error);
	}

	.gate-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}

	.gate-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.gate-status {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.gate-status.success {
		color: var(--color-success);
	}

	.gate-status.error {
		color: var(--color-error);
	}

	.gate-status.loading {
		color: var(--color-info);
	}

	.gate-status.neutral {
		color: var(--color-text-muted);
	}

	.gate-summary {
		margin-top: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-relaxed);
	}

	.badge {
		display: inline-flex;
		align-items: center;
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-full);
	}

	.badge-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.badge-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.badge-warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.badge-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
