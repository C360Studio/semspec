<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import {
		formatCostLabel,
		formatRateSourceLabel,
		type ProviderRate
	} from '$lib/types/costAccounting';
	import {
		formatToken,
		lessonActivityModel
	} from '$lib/components/plan/observabilityModels';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { TrajectoryListItem } from '$lib/types/trajectory';

	interface Props {
		plan: PlanWithStatus;
		trajectoryItems?: TrajectoryListItem[];
		providerRates?: ProviderRate[];
	}

	let { plan, trajectoryItems = [], providerRates = [] }: Props = $props();

	const lessonSummary = $derived(plan.phase_summary?.lessons ?? null);
	const model = $derived(lessonActivityModel(plan, trajectoryItems, providerRates));

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60000).toFixed(1)}m`;
	}
</script>

<section class="lesson-detail" aria-label="Lesson activity detail">
	<div class="section-header">
		<div class="section-title">
			<Icon name="lightbulb" size={14} />
			<span>Lesson Activity</span>
		</div>
		{#if lessonSummary?.state}
			<span class="status-pill">{formatToken(lessonSummary.state)}</span>
		{/if}
	</div>

	<div class="effect-grid">
		<div class="effect-cell" data-effect={lessonSummary?.current_run_effect ?? 'none'}>
			<span class="field-label">Current Run</span>
			<strong>{model.currentEffect}</strong>
		</div>
		<div class="effect-cell">
			<span class="field-label">Future Runs</span>
			<strong>{model.futureEffect}</strong>
		</div>
		<div class="effect-cell" title={formatRateSourceLabel(model.costAccounting)}>
			<span class="field-label">Lesson Cost</span>
			<strong>{formatCostLabel(model.costAccounting, true)}</strong>
		</div>
	</div>

	{#if lessonSummary?.detail}
		<p class="lesson-detail-text">{lessonSummary.detail}</p>
	{/if}

	{#if model.lessonLoops.length > 0}
		<div class="metric-row">
			<span>{model.lessonLoops.length} loop{model.lessonLoops.length === 1 ? '' : 's'}</span>
			<span>{formatTokens(model.lessonUsage.totalTokens)} tokens</span>
			<span>{formatRateSourceLabel(model.costAccounting)}</span>
		</div>

		<div class="role-list">
			{#each model.roleSummaries as role}
				<div class="role-row">
					<span>{role.role}</span>
					<span>{role.loops} loop{role.loops === 1 ? '' : 's'}</span>
					<span>{formatTokens(role.tokens)} tok</span>
					<span>{formatDuration(role.duration)}</span>
				</div>
			{/each}
		</div>
	{:else}
		<div class="empty-note">
			<Icon name="info" size={16} />
			<span>Lesson activity has not produced measured trajectory usage for this run.</span>
		</div>
	{/if}
</section>

<style>
	.lesson-detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-bg-secondary);
	}

	.section-header,
	.section-title,
	.metric-row,
	.role-row,
	.empty-note {
		display: flex;
		align-items: center;
	}

	.section-header {
		justify-content: space-between;
		gap: var(--space-3);
	}

	.section-title {
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.status-pill {
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
	}

	.effect-grid {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: var(--space-2);
	}

	.effect-cell {
		min-width: 0;
		padding: var(--space-2);
		border: 1px solid var(--color-border-subtle, var(--color-border));
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
	}

	.effect-cell[data-effect='none'] strong {
		color: var(--color-warning);
	}

	.field-label {
		display: block;
		margin-bottom: var(--space-1);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--color-text-muted);
	}

	.effect-cell strong {
		display: block;
		overflow-wrap: anywhere;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.lesson-detail-text {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-normal);
		color: var(--color-text-secondary);
	}

	.metric-row {
		flex-wrap: wrap;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.role-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.role-row {
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-md);
		background: var(--color-bg-tertiary);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.role-row span:first-child {
		color: var(--color-text-primary);
		font-weight: var(--font-weight-medium);
	}

	.empty-note {
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-3);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	@media (max-width: 720px) {
		.effect-grid {
			grid-template-columns: 1fr;
		}

		.role-row {
			flex-wrap: wrap;
			justify-content: flex-start;
		}
	}
</style>
