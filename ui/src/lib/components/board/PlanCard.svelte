<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import PipelineIndicator from './PipelineIndicator.svelte';
	import ModeIndicator from './ModeIndicator.svelte';
	import AgentBadge from './AgentBadge.svelte';
	import LoopHeartbeat from './LoopHeartbeat.svelte';
	import { derivePlanPipeline, getStageLabel, type PlanWithStatus } from '$lib/types/plan';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { feedStore } from '$lib/stores/feed.svelte';
	import { executePlan } from '$lib/actions/plans';
	import type { Task } from '$lib/types/task';

	interface Props {
		plan: PlanWithStatus;
		tasks?: Task[];
	}

	let { plan, tasks = [] }: Props = $props();

	const pipeline = $derived(derivePlanPipeline(plan));
	const isDraft = $derived(!plan.approved);

	// Cascade is actively running (LLM working) — spin the indicator
	const isBusy = $derived(
		['drafting', 'planning', 'approved', 'requirements_generated'].includes(plan.stage)
	);
	const hasRejection = $derived(
		(plan.active_loops ?? []).some((l) => l.current_task_id) &&
			tasks.some((t) => t.rejection)
	);

	// Count pending questions for this plan's loops
	const planLoopIds = $derived((plan.active_loops ?? []).map((l) => l.loop_id));
	const questionCount = $derived(
		questionsStore.pending.filter(
			(q) => q.blocked_loop_id && planLoopIds.includes(q.blocked_loop_id)
		).length
	);

	// Count dirty tasks (need re-evaluation after a PlanDecision cascade)
	const dirtyTaskCount = $derived(
		tasks.filter((t) => t.status === 'dirty').length
	);

	// Liveness signal for implementing plans: find the most-recently-updated task
	// SSE payload for one of this plan's active loops and surface its TDD cycle
	// plus "last update Nm ago". Answers "is this thing working?" without a detail drill-down.
	const activeTaskStatus = $derived.by(() => {
		if (plan.stage !== 'implementing') return null;
		const taskIds = (plan.active_loops ?? [])
			.map((l) => l.current_task_id)
			.filter((id): id is string => !!id);
		if (taskIds.length === 0) return null;
		let latest: { task_id: string; tdd_cycle?: number; max_tdd_cycles?: number; updated_at?: string } | null = null;
		let latestTs = 0;
		for (const id of taskIds) {
			const payload = feedStore.taskStages.get(id);
			if (!payload) continue;
			const ts = payload.updated_at ? Date.parse(payload.updated_at) : 0;
			if (!latest || ts > latestTs) {
				latest = payload;
				latestTs = ts;
			}
		}
		return latest;
	});

	// Stalled = no forward motion for 10+ minutes while implementing. Small-LLM
	// runs churn and the user needs a clear "something is wrong" signal vs the
	// subtle "Nm ago" text. Border treatment mirrors has-rejection for consistency.
	const STALLED_THRESHOLD_MS = 10 * 60 * 1000;
	const isStalled = $derived.by(() => {
		if (!activeTaskStatus?.updated_at) return false;
		const age = Date.now() - Date.parse(activeTaskStatus.updated_at);
		return Number.isFinite(age) && age > STALLED_THRESHOLD_MS;
	});

	function ageLabel(iso: string | undefined): string {
		if (!iso) return '';
		const ms = Date.now() - Date.parse(iso);
		if (Number.isNaN(ms) || ms < 0) return '';
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s ago`;
		const m = Math.floor(s / 60);
		if (m < 60) return `${m}m ago`;
		const h = Math.floor(m / 60);
		return `${h}h ago`;
	}

	async function handleExecute(e: Event) {
		e.preventDefault();
		e.stopPropagation();
		await executePlan(plan.slug);
	}
</script>

<a
	href="/plans/{plan.slug}"
	class="plan-card"
	class:draft={isDraft}
	class:has-rejection={hasRejection}
	class:stalled={isStalled}
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
			{#if dirtyTaskCount > 0}
				<span class="dirty-badge" title="{dirtyTaskCount} task{dirtyTaskCount !== 1 ? 's' : ''} need re-evaluation">
					<Icon name="alert-circle" size={12} />
					{dirtyTaskCount} dirty
				</span>
			{/if}
		</div>
		<ModeIndicator approved={plan.approved} busy={isBusy} compact />
	</div>

	<div class="stage-row">
		<span class="stage-label" data-stage={plan.stage}>{getStageLabel(plan.stage)}</span>
		{#if plan.approved}
			<PipelineIndicator
				plan={pipeline.plan}
				requirements={pipeline.requirements}
				execute={pipeline.execute}
				compact
			/>
		{/if}
	</div>

	{#if activeTaskStatus}
		<div class="liveness-row" data-testid="plan-card-liveness">
			{#if typeof activeTaskStatus.tdd_cycle === 'number' && typeof activeTaskStatus.max_tdd_cycles === 'number'}
				<span class="cycle-badge" title="TDD rework cycle">
					cycle {activeTaskStatus.tdd_cycle + 1}/{activeTaskStatus.max_tdd_cycles}
				</span>
			{/if}
			{#if activeTaskStatus.updated_at}
				<span class="liveness-age" title="Time since the last state change">
					{ageLabel(activeTaskStatus.updated_at)}
				</span>
			{/if}
		</div>
	{:else if (plan.active_loops ?? []).length > 0}
		<!-- Generation phases (planning, requirements, scenarios, reviews) don't
		     have TaskExecution data yet, but they DO have active agent loops.
		     LoopHeartbeat surfaces "is this thing on?" from the activity SSE
		     tick timeline — so humans watching a 15-minute scenario phase
		     see a live pulse and "idle Ns" counter instead of deafening silence. -->
		<LoopHeartbeat loops={plan.active_loops ?? []} />
	{/if}

	{#if plan.stage === 'scenarios_generated'}
		<div class="action-row">
			<span class="review-btn">
				<Icon name="eye" size={14} />
				Review
			</span>
		</div>
	{:else if plan.stage === 'ready_for_execution'}
		<div class="action-row">
			<button class="execute-btn" onclick={handleExecute}>
				<Icon name="play" size={14} />
				Run
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

	.plan-card.stalled {
		border-left: 3px solid var(--color-error);
	}

	.plan-card.stalled .liveness-age {
		color: var(--color-error);
		font-weight: var(--font-weight-semibold);
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

	.dirty-badge {
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

	.liveness-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.cycle-badge {
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		font-family: var(--font-family-mono);
	}

	.liveness-age {
		font-family: var(--font-family-mono);
	}

	.stage-row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}

	.stage-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.stage-label[data-stage='implementing'],
	.stage-label[data-stage='executing'] {
		color: var(--color-accent);
	}

	.stage-label[data-stage='complete'] {
		color: var(--color-success);
	}

	.stage-label[data-stage='failed'] {
		color: var(--color-error);
	}

	.action-row {
		margin-bottom: var(--space-2);
	}

	.review-btn,
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
		text-decoration: none;
		transition: opacity var(--transition-fast);
	}

	.review-btn {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border: 1px solid var(--color-accent);
	}

	.execute-btn {
		background: var(--color-success);
		color: white;
	}

	.review-btn:hover,
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
