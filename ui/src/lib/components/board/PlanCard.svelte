<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import PipelineIndicator from './PipelineIndicator.svelte';
	import ModeIndicator from './ModeIndicator.svelte';
	import AgentBadge from './AgentBadge.svelte';
	import { derivePlanPipeline, type PlanWithStatus } from '$lib/types/plan';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';

	interface Props {
		plan: PlanWithStatus;
	}

	let { plan }: Props = $props();

	const pipeline = $derived(derivePlanPipeline(plan));
	const isDraft = $derived(!plan.approved);
	const hasRejection = $derived(
		(plan.active_loops ?? []).some((l) => l.current_task_id) &&
			plansStore.getTasks(plan.slug).some((t) => t.rejection)
	);

	// Count pending questions for this plan's loops
	const planLoopIds = $derived((plan.active_loops ?? []).map((l) => l.loop_id));
	const questionCount = $derived(
		questionsStore.pending.filter(
			(q) => q.blocked_loop_id && planLoopIds.includes(q.blocked_loop_id)
		).length
	);

	async function handlePromote(e: Event) {
		e.preventDefault();
		e.stopPropagation();
		await plansStore.promote(plan.slug);
	}

	async function handleExecute(e: Event) {
		e.preventDefault();
		e.stopPropagation();
		await plansStore.execute(plan.slug);
	}
</script>

<a
	href="/plans/{plan.slug}"
	class="plan-card"
	class:draft={isDraft}
	class:has-rejection={hasRejection}
>
	<div class="card-header">
		<div class="title-row">
			<h3 class="plan-title">{plan.slug}</h3>
			{#if questionCount > 0}
				<span class="question-badge" title="{questionCount} pending question{questionCount !== 1 ? 's' : ''}">
					<Icon name="help-circle" size={12} />
					{questionCount}
				</span>
			{/if}
		</div>
		<ModeIndicator approved={plan.approved} compact />
	</div>

	{#if plan.approved}
		<div class="pipeline-row">
			<PipelineIndicator
				plan={pipeline.plan}
				tasks={pipeline.tasks}
				execute={pipeline.execute}
			/>
			{#if plan.task_stats}
				<span class="task-count">
					{plan.task_stats.completed}/{plan.task_stats.total} tasks
				</span>
			{/if}
		</div>
	{:else}
		<div class="draft-row">
			<span class="draft-label">Pending approval...</span>
		</div>
	{/if}

	{#if (plan.active_loops ?? []).length > 0}
		<div class="agents-row">
			{#each plan.active_loops ?? [] as loop}
				<AgentBadge
					role={loop.role}
					model={loop.model}
					state={loop.state}
					iterations={loop.iterations}
					maxIterations={loop.max_iterations}
				/>
			{/each}
		</div>
	{/if}

	{#if isDraft && plan.goal}
		<div class="action-row">
			<button class="promote-btn" onclick={handlePromote}>
				<Icon name="check" size={14} />
				Approve Plan
			</button>
		</div>
	{/if}

	{#if plan.stage === 'tasks' && plan.task_stats && plan.task_stats.total > 0}
		<div class="action-row">
			<button class="execute-btn" onclick={handleExecute}>
				<Icon name="play" size={14} />
				Start Execution
			</button>
		</div>
	{/if}

	{#if plan.github}
		<div class="github-row">
			<Icon name="external-link" size={12} />
			<span>GH #{plan.github.epic_number}</span>
		</div>
	{/if}
</a>

<style>
	.plan-card {
		display: block;
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		text-decoration: none;
		color: inherit;
		transition: all var(--transition-fast);
	}

	.plan-card:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
		text-decoration: none;
	}

	.plan-card:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
		border-color: var(--color-accent);
	}

	.plan-card.draft {
		border-style: dashed;
		background: var(--color-bg-primary);
	}

	.plan-card.draft:hover {
		background: var(--color-bg-secondary);
	}

	.plan-card.has-rejection {
		border-left: 3px solid var(--color-warning);
	}

	.card-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-3);
	}

	.title-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.plan-title {
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.question-badge {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		padding: 2px 6px;
		background: var(--color-warning-muted);
		color: var(--color-warning);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
	}

	.pipeline-row {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		margin-bottom: var(--space-3);
	}

	.draft-row {
		margin-bottom: var(--space-3);
	}

	.draft-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		font-style: italic;
	}

	.task-count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.agents-row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.action-row {
		margin-bottom: var(--space-2);
	}

	.promote-btn,
	.execute-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.promote-btn {
		background: var(--color-accent);
		color: white;
	}

	.execute-btn {
		background: var(--color-success);
		color: white;
	}

	.promote-btn:hover,
	.execute-btn:hover {
		opacity: 0.9;
	}

	.github-row {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
