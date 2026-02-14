<script lang="ts">
	import { page } from '$app/stores';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PlanPanel from '$lib/components/changes/PlanPanel.svelte';
	import TaskList from '$lib/components/changes/TaskList.svelte';
	import RejectionBanner from '$lib/components/changes/RejectionBanner.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import { onMount } from 'svelte';

	const slug = $derived($page.params.slug);
	const plan = $derived(slug ? plansStore.getBySlug(slug) : undefined);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);

	let tasks = $state<Task[]>([]);

	onMount(async () => {
		await plansStore.fetch();
		if (slug) {
			tasks = await plansStore.fetchTasks(slug);
		}
	});

	// Find any task with an active rejection
	const activeRejection = $derived.by(() => {
		const rejectedTask = tasks.find((t) => t.rejection && t.status === 'in_progress');
		return rejectedTask ? { task: rejectedTask, rejection: rejectedTask.rejection! } : null;
	});

	async function handlePromote() {
		if (plan) {
			await plansStore.promote(plan.slug);
		}
	}

	async function handleGenerateTasks() {
		if (plan) {
			await plansStore.generateTasks(plan.slug);
		}
	}

	async function handleExecute() {
		if (plan) {
			await plansStore.execute(plan.slug);
		}
	}

	function getStageLabel(stage: PlanStage): string {
		switch (stage) {
			case 'exploration':
				return 'Exploring';
			case 'planning':
				return 'Planning';
			case 'tasks':
				return 'Ready to Execute';
			case 'executing':
				return 'Executing';
			case 'complete':
				return 'Complete';
			case 'failed':
				return 'Failed';
		}
	}
</script>

<svelte:head>
	<title>{plan?.slug || 'Plan'} - Semspec</title>
</svelte:head>

<div class="plan-detail">
	<header class="detail-header">
		<a href="/plans" class="back-link">
			<Icon name="chevron-left" size={16} />
			Back to Plans
		</a>
	</header>

	{#if !plan}
		<div class="not-found">
			<Icon name="alert-circle" size={48} />
			<h2>Plan not found</h2>
			<p>The plan "{slug}" could not be found.</p>
			<a href="/plans" class="btn btn-primary">Back to Plans</a>
		</div>
	{:else}
		<div class="plan-info">
			<h1 class="plan-title">{plan.title || plan.slug}</h1>
			<div class="plan-meta">
				<ModeIndicator committed={plan.committed} />
				<span class="plan-stage" data-stage={plan.stage}>
					{getStageLabel(plan.stage)}
				</span>
				{#if plan.github}
					<span class="separator">|</span>
					<a
						href={plan.github.epic_url}
						target="_blank"
						rel="noopener noreferrer"
						class="github-link"
					>
						<Icon name="external-link" size={14} />
						GH #{plan.github.epic_number}
					</a>
				{/if}
			</div>
		</div>

		{#if pipeline && plan.committed}
			<div class="pipeline-section">
				<PipelineIndicator
					plan={pipeline.plan}
					tasks={pipeline.tasks}
					execute={pipeline.execute}
				/>
			</div>
		{/if}

		{#if activeRejection}
			<RejectionBanner
				rejection={activeRejection.rejection}
				taskDescription={activeRejection.task.description}
			/>
		{/if}

		{#if !plan.committed && plan.goal}
			<div class="action-banner promote">
				<Icon name="arrow-up" size={20} />
				<div class="action-content">
					<strong>Ready to promote</strong>
					<p>This exploration has enough context. Promote it to a committed plan to generate tasks.</p>
				</div>
				<button class="btn btn-primary" onclick={handlePromote}>
					Promote to Plan
				</button>
			</div>
		{/if}

		{#if plan.committed && plan.stage === 'planning'}
			<div class="action-banner generate">
				<Icon name="list" size={20} />
				<div class="action-content">
					<strong>Ready to generate tasks</strong>
					<p>The plan is committed. Generate implementation tasks to start execution.</p>
				</div>
				<button class="btn btn-primary" onclick={handleGenerateTasks}>
					Generate Tasks
				</button>
			</div>
		{/if}

		{#if plan.stage === 'tasks' && tasks.length > 0}
			<div class="action-banner execute">
				<Icon name="play" size={20} />
				<div class="action-content">
					<strong>Tasks ready</strong>
					<p>{tasks.length} tasks generated. Start execution to begin implementation.</p>
				</div>
				<button class="btn btn-success" onclick={handleExecute}>
					Start Execution
				</button>
			</div>
		{/if}

		<div class="detail-content">
			<div class="plan-section">
				<PlanPanel {plan} />
			</div>

			<div class="tasks-section">
				<TaskList {tasks} activeLoops={plan.active_loops} />
			</div>
		</div>
	{/if}
</div>

<style>
	.plan-detail {
		padding: var(--space-6);
		max-width: 1200px;
		margin: 0 auto;
	}

	.detail-header {
		margin-bottom: var(--space-4);
	}

	.back-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-muted);
		text-decoration: none;
		font-size: var(--font-size-sm);
	}

	.back-link:hover {
		color: var(--color-text-primary);
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

	.plan-info {
		margin-bottom: var(--space-4);
	}

	.plan-title {
		font-size: var(--font-size-2xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0 0 var(--space-2);
	}

	.plan-meta {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.plan-stage {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.plan-stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.separator {
		color: var(--color-border);
	}

	.github-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-accent);
	}

	.pipeline-section {
		padding: var(--space-4) 0;
		margin-bottom: var(--space-4);
	}

	.action-banner {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		padding: var(--space-4);
		border: 1px solid;
		border-radius: var(--radius-lg);
		margin-bottom: var(--space-6);
	}

	.action-banner.promote {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.action-banner.generate {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.action-banner.execute {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.1));
		border-color: var(--color-success);
		color: var(--color-success);
	}

	.action-content {
		flex: 1;
	}

	.action-content strong {
		display: block;
		color: var(--color-text-primary);
	}

	.action-content p {
		margin: var(--space-1) 0 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.btn {
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
		text-decoration: none;
	}

	.btn:hover {
		opacity: 0.9;
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-success {
		background: var(--color-success);
		color: white;
	}

	.detail-content {
		display: grid;
		grid-template-columns: 300px 1fr;
		gap: var(--space-6);
		padding-top: var(--space-6);
		border-top: 1px solid var(--color-border);
	}

	.plan-section,
	.tasks-section {
		overflow: hidden;
	}

	@media (max-width: 768px) {
		.detail-content {
			grid-template-columns: 1fr;
		}
	}
</style>
