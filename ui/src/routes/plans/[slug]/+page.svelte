<script lang="ts">
	import { page } from '$app/state';
	import { invalidate } from '$app/navigation';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import PlanDetail from '$lib/components/plan/PlanDetail.svelte';
	import RequirementPanel from '$lib/components/plan/RequirementPanel.svelte';
	import RejectionBanner from '$lib/components/plan/RejectionBanner.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import { api } from '$lib/api/client';
	import { promotePlan, executePlan } from '$lib/actions/plans';
	import { derivePlanPipeline, getStageLabel } from '$lib/types/plan';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const slug = $derived(page.params.slug);
	const plan = $derived(data.plan);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);
	const tasks = $derived(data.tasks);

	const activeRejection = $derived.by(() => {
		const rejectedTask = tasks.find((t) => t.rejection && t.status === 'in_progress');
		return rejectedTask ? { task: rejectedTask, rejection: rejectedTask.rejection! } : null;
	});

	// Cascade stages that need periodic refresh
	const isCascading = $derived(
		plan !== null && ['approved', 'requirements_generated', 'scenarios_generated'].includes(plan.stage)
	);

	$effect(() => {
		if (!isCascading) return;
		const interval = setInterval(() => invalidate('app:plans'), 5000);
		return () => clearInterval(interval);
	});

	// Plan shows requirements when approved
	const showRequirements = $derived(plan?.approved === true);

	let actionError = $state<string | null>(null);

	async function handlePromote() {
		if (!plan) return;
		actionError = null;
		try {
			await promotePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to approve plan';
		}
	}

	async function handleExecute() {
		if (!plan) return;
		actionError = null;
		try {
			await executePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to start execution';
		}
	}

	async function handleReplay() {
		if (!plan) return;
		actionError = null;
		try {
			await executePlan(plan.slug);
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Failed to replay';
		}
	}

	async function handleRefresh() {
		await invalidate('app:plans');
	}
</script>

<svelte:head>
	<title>{plan?.title || plan?.slug || 'Plan'} - Semspec</title>
</svelte:head>

<div class="plan-detail">
	<header class="detail-header">
		<a href="/" class="back-link">
			<Icon name="chevron-left" size={16} />
			Back
		</a>
		{#if plan}
			<div class="header-info">
				<h1 class="plan-title">{plan.title || plan.slug}</h1>
				<div class="plan-meta">
					<ModeIndicator approved={plan.approved} />
					<span class="plan-stage" data-stage={plan.stage}>
						{getStageLabel(plan.stage)}
					</span>
				</div>
			</div>
		{/if}
	</header>

	{#if !plan}
		<div class="not-found">
			<Icon name="alert-circle" size={48} />
			<h2>Plan not found</h2>
			<p>The plan "{slug}" could not be found.</p>
			<a href="/" class="btn btn-primary">Back to Board</a>
		</div>
	{:else}
		<div class="plan-content">
			<!-- Action bar: approve / execute / status -->
			{#if plan.goal || plan.approved}
				<div class="action-row">
					{#if plan.approved && pipeline}
						<PipelineIndicator
							plan={pipeline.plan}
							requirements={pipeline.requirements}
							execute={pipeline.execute}
						/>
					{/if}
					<ActionBar
						{plan}
						onPromote={handlePromote}
						onExecute={handleExecute}
						onReplay={handleReplay}
					/>
				</div>
			{/if}

			{#if actionError}
				<div class="error-banner" role="alert">
					<Icon name="alert-circle" size={14} />
					<span>{actionError}</span>
				</div>
			{/if}

			<!-- Agent pipeline during execution -->
			{#if plan.active_loops && plan.active_loops.length > 0}
				<div class="pipeline-section">
					<AgentPipelineView slug={plan.slug} loops={plan.active_loops} />
				</div>
			{/if}

			{#if activeRejection}
				<RejectionBanner
					rejection={activeRejection.rejection}
					taskDescription={activeRejection.task.description}
				/>
			{/if}

			<!-- Plan details: goal, context, scope -->
			<PlanDetail {plan} phases={[]} requirements={[]} onRefresh={handleRefresh} />

			<!-- Requirements + Scenarios (shown when plan is approved) -->
			{#if showRequirements}
				<div class="requirements-section">
					<RequirementPanel slug={plan.slug} />
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.plan-detail {
		padding: var(--space-4) var(--space-6);
		max-width: 900px;
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.detail-header {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		margin-bottom: var(--space-4);
		flex-shrink: 0;
	}

	.back-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-muted);
		text-decoration: none;
		font-size: var(--font-size-sm);
		flex-shrink: 0;
	}

	.back-link:hover {
		color: var(--color-text-primary);
	}

	.header-info {
		flex: 1;
		min-width: 0;
	}

	.plan-title {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.plan-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}

	.plan-stage {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.plan-stage[data-stage='implementing'],
	.plan-stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='ready_for_execution'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.not-found {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-12) 0;
		color: var(--color-text-muted);
		text-align: center;
	}

	.not-found h2 {
		margin: 0;
		color: var(--color-text-primary);
	}

	.plan-content {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.action-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-lg);
		flex-shrink: 0;
	}

	.error-banner {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
	}

	.pipeline-section {
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
	}

	.requirements-section {
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-4);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		text-decoration: none;
		cursor: pointer;
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover {
		opacity: 0.9;
	}

	@media (max-width: 768px) {
		.plan-detail {
			padding: var(--space-3);
		}

		.action-row {
			flex-direction: column;
			align-items: stretch;
		}
	}
</style>
