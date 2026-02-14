<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import SpecGate from './SpecGate.svelte';
	import ReviewerCard from './ReviewerCard.svelte';
	import FindingsList from './FindingsList.svelte';
	import { reviewsStore } from '$lib/stores/reviews.svelte';
	import type { SynthesisResult } from '$lib/types/review';
	import {
		getVerdictLabel,
		getVerdictClass,
		getSpecReviewer,
		getQualityReviewers,
		sortFindingsBySeverity
	} from '$lib/types/review';

	interface Props {
		/** Plan slug to show reviews for */
		slug: string;
		/** Optional pre-loaded result (skips fetch) */
		result?: SynthesisResult;
		/** Whether to auto-fetch on mount */
		autoFetch?: boolean;
	}

	let { slug, result: externalResult, autoFetch = true }: Props = $props();

	// Use external result if provided, otherwise fetch from store
	const result = $derived(externalResult || reviewsStore.get(slug));
	const loading = $derived(reviewsStore.loading);
	const error = $derived(reviewsStore.error);

	// Derived data from result
	const specReviewer = $derived(result ? getSpecReviewer(result) : undefined);
	const qualityReviewers = $derived(result ? getQualityReviewers(result) : []);
	const findings = $derived(result ? sortFindingsBySeverity(result.findings) : []);
	const specGatePassed = $derived(specReviewer?.passed ?? false);

	// Fetch on mount if autoFetch and no external result
	$effect(() => {
		if (autoFetch && !externalResult && !reviewsStore.has(slug)) {
			reviewsStore.fetch(slug);
		}
	});

	function handleRefresh() {
		reviewsStore.fetch(slug);
	}
</script>

<div class="review-dashboard">
	<div class="dashboard-header">
		<h2 class="dashboard-title">
			<Icon name="list-checks" size={20} />
			Review Results
		</h2>

		<div class="header-actions">
			{#if result}
				<span class="badge badge-{getVerdictClass(result.verdict)}">
					{getVerdictLabel(result.verdict)}
				</span>
			{/if}
			<button class="btn-icon" onclick={handleRefresh} disabled={loading} title="Refresh">
				<Icon name="refresh-cw" size={16} class={loading ? 'spin' : ''} />
			</button>
		</div>
	</div>

	{#if error}
		<div class="error-state">
			<Icon name="alert-triangle" size={24} />
			<p>{error}</p>
			<button class="btn btn-secondary" onclick={handleRefresh}>Retry</button>
		</div>
	{:else if loading && !result}
		<div class="loading-state">
			<Icon name="loader" size={24} class="spin" />
			<p>Loading review results...</p>
		</div>
	{:else if result}
		<!-- Stage 1: Spec Compliance Gate -->
		<section class="review-stage">
			<SpecGate reviewer={specReviewer} loading={false} />
		</section>

		<!-- Stage 2: Quality Reviewers (only show if spec gate passed or we have results) -->
		{#if specGatePassed || qualityReviewers.length > 0}
			<div class="stage-connector">
				<div class="connector-line"></div>
				<Icon name="chevron-down" size={16} />
				<div class="connector-line"></div>
			</div>

			<section class="review-stage">
				<div class="stage-header">
					<h3 class="stage-title">Stage 2: Quality Reviews</h3>
					<span class="pass-count">
						{result.stats.reviewers_passed}/{result.stats.reviewers_total} passed
					</span>
				</div>

				<div class="reviewers-grid">
					{#each qualityReviewers as reviewer}
						<ReviewerCard {reviewer} />
					{/each}
				</div>
			</section>
		{/if}

		<!-- Findings Section -->
		{#if findings.length > 0}
			<div class="stage-connector">
				<div class="connector-line"></div>
				<Icon name="chevron-down" size={16} />
				<div class="connector-line"></div>
			</div>

			<section class="review-stage">
				<FindingsList {findings} stats={result.stats} showHeader={true} />
			</section>
		{/if}

		<!-- Partial Result Warning -->
		{#if result.partial}
			<div class="partial-warning">
				<Icon name="alert-triangle" size={16} />
				<span>
					Partial result: {result.missing_reviewers?.join(', ')} did not respond in time
				</span>
			</div>
		{/if}

		<!-- Summary -->
		{#if result.summary}
			<div class="result-summary">
				<Icon name="info" size={16} />
				<p>{result.summary}</p>
			</div>
		{/if}
	{:else}
		<div class="empty-state">
			<Icon name="inbox" size={32} />
			<p>No review results available</p>
			<span class="empty-hint">Reviews will appear here after code review completes</span>
		</div>
	{/if}
</div>

<style>
	.review-dashboard {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.dashboard-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.dashboard-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.badge {
		display: inline-flex;
		align-items: center;
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-sm);
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

	.btn-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-icon:hover:not(:disabled) {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.btn-icon:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.review-stage {
		/* Container for each stage - styles applied via children */
		display: block;
	}

	.stage-connector {
		display: flex;
		flex-direction: column;
		align-items: center;
		color: var(--color-text-muted);
		padding: var(--space-1) 0;
	}

	.connector-line {
		width: 1px;
		height: var(--space-2);
		background: var(--color-border);
	}

	.stage-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}

	.stage-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.pass-count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.reviewers-grid {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
	}

	.loading-state,
	.error-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-8);
		text-align: center;
	}

	.loading-state {
		color: var(--color-info);
	}

	.loading-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.error-state {
		color: var(--color-error);
	}

	.error-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.empty-state {
		color: var(--color-text-muted);
	}

	.empty-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.empty-hint {
		font-size: var(--font-size-sm);
	}

	.partial-warning {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-warning-muted);
		border: 1px solid var(--color-warning);
		border-radius: var(--radius-md);
		color: var(--color-warning);
		font-size: var(--font-size-sm);
	}

	.result-summary {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
	}

	.result-summary p {
		margin: 0;
		line-height: var(--line-height-normal);
	}

	.result-summary :global(svg) {
		flex-shrink: 0;
		margin-top: 2px;
	}

	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-md);
		border: 1px solid transparent;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-secondary {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
		border-color: var(--color-border);
	}

	.btn-secondary:hover {
		background: var(--color-bg-elevated);
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
