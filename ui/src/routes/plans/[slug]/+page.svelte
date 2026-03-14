<script lang="ts">
	import { page } from '$app/state';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import PlanNavTree from '$lib/components/plan/PlanNavTree.svelte';
	import PlanDetailPanel from '$lib/components/plan/PlanDetailPanel.svelte';
	import RejectionBanner from '$lib/components/plan/RejectionBanner.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import { ReviewDashboard } from '$lib/components/review';
	import TrajectoryPanel from '$lib/components/trajectory/TrajectoryPanel.svelte';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import { chatBarStore } from '$lib/stores/chatDrawer.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { planSelectionStore, type PlanSelection } from '$lib/stores/planSelection.svelte';
	import { api } from '$lib/api/client';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import type { Phase } from '$lib/types/phase';
	import type { Requirement } from '$lib/types/requirement';
	import type { Scenario } from '$lib/types/scenario';
	import { onMount } from 'svelte';

	const slug = $derived(page.params.slug);
	const plan = $derived(slug ? plansStore.getBySlug(slug) : undefined);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);

	let tasks = $state<Task[]>([]);
	let phases = $state<Phase[]>([]);
	let requirements = $state<Requirement[]>([]);
	let scenariosByReq = $state<Record<string, Scenario[]>>({});
	let activeTab = $state<'nav' | 'detail'>('nav');

	// Group tasks by phase ID for legacy nav tree support
	const tasksByPhase = $derived.by(() => {
		const grouped: Record<string, Task[]> = {};
		for (const task of tasks) {
			const phaseId = task.phase_id ?? '__unassigned__';
			if (!grouped[phaseId]) {
				grouped[phaseId] = [];
			}
			grouped[phaseId].push(task);
		}
		return grouped;
	});

	// Selection state - initialize to plan
	$effect(() => {
		if (slug && !planSelectionStore.selection) {
			planSelectionStore.selectPlan(slug);
		}
	});

	// Update label cache when plan/requirements/scenarios change
	$effect(() => {
		if (plan) {
			planSelectionStore.setLabel(`plan:${plan.slug}`, plan.title || plan.slug);
		}
		for (const req of requirements) {
			planSelectionStore.setLabel(`requirement:${req.id}`, req.title);
		}
		for (const [reqId, scenarios] of Object.entries(scenariosByReq)) {
			for (const scenario of scenarios) {
				const label = `When ${scenario.when}`.slice(0, 30);
				planSelectionStore.setLabel(`scenario:${scenario.id}`, label);
			}
		}
		// Legacy labels
		for (const phase of phases) {
			planSelectionStore.setLabel(`phase:${phase.id}`, phase.name);
		}
		for (const task of tasks) {
			const label = task.description.length > 30
				? task.description.slice(0, 30) + '...'
				: task.description;
			planSelectionStore.setLabel(`task:${task.id}`, label);
		}
	});

	// Show reviews / trajectory in right panel when plan is executing or complete
	const canShowReviews = $derived(
		plan?.approved &&
			(plan?.stage === 'implementing' ||
				plan?.stage === 'executing' ||
				plan?.stage === 'complete')
	);

	// The most recent active loop ID for TrajectoryPanel
	const activeLoopId = $derived.by(() => {
		const loops = plan?.active_loops ?? [];
		return loops.length > 0 ? loops[0].loop_id : null;
	});

	// Right panel should open automatically when there's context to show
	const rightPanelShouldOpen = $derived(canShowReviews || activeLoopId !== null);

	onMount(() => {
		// Initial data fetch
		plansStore.fetch().then(() => {
			if (slug) {
				// Fetch requirements and scenarios
				fetchRequirements();
				// Legacy: fetch phases and tasks
				plansStore.fetchTasks(slug).then((fetched) => {
					tasks = fetched;
				});
				api.phases.list(slug).then((fetched) => {
					phases = fetched;
				}).catch(() => {
					phases = [];
				});
			}
		});
		// Fetch questions for QuestionQueue
		questionsStore.fetch('pending');
		const interval = setInterval(() => {
			questionsStore.fetch('pending');
			// Periodically refresh requirements during cascade
			if (plan && ['approved', 'requirements_generated', 'scenarios_generated'].includes(plan.stage)) {
				fetchRequirements();
				plansStore.fetch();
			}
		}, 5000);

		return () => {
			clearInterval(interval);
			planSelectionStore.clear();
		};
	});

	async function fetchRequirements(): Promise<void> {
		if (!slug) return;
		try {
			requirements = await api.requirements.list(slug);
		} catch {
			requirements = [];
		}
	}

	async function fetchScenariosForReq(reqId: string): Promise<void> {
		if (!slug) return;
		try {
			const scenarios = await api.scenarios.listByRequirement(slug, reqId);
			scenariosByReq = { ...scenariosByReq, [reqId]: scenarios };
		} catch {
			scenariosByReq = { ...scenariosByReq, [reqId]: [] };
		}
	}

	// Get plan's loop IDs for filtering questions
	const planLoopIds = $derived.by(() => {
		if (!plan) return [];
		return (plan.active_loops ?? []).map((l) => l.loop_id);
	});

	// Filter questions to this plan's loops
	const planQuestions = $derived(
		questionsStore.pending.filter(
			(q) => q.blocked_loop_id && planLoopIds.includes(q.blocked_loop_id)
		)
	);

	function handleAnswerQuestion(questionId: string): void {
		chatBarStore.open({ type: 'question', questionId, planSlug: slug });
	}

	// Inform the bottom chat bar which plan is currently active
	$effect(() => {
		if (slug && plan) {
			chatBarStore.setContext({ type: 'plan', planSlug: slug });
			chatBarStore.setPageContext([
				{ type: 'plan', id: slug, label: plan.title || slug }
			]);
		}
		return () => {
			chatBarStore.clearPageContext();
		};
	});

	// Find any task with an active rejection
	const activeRejection = $derived.by(() => {
		const rejectedTask = tasks.find((t) => t.rejection && t.status === 'in_progress');
		return rejectedTask ? { task: rejectedTask, rejection: rejectedTask.rejection! } : null;
	});

	function handleSelect(selection: PlanSelection): void {
		planSelectionStore.selection = selection;
	}

	// Action handlers
	async function handlePromote() {
		if (plan) {
			await plansStore.promote(plan.slug);
		}
	}

	async function handleExecute() {
		if (plan) {
			await plansStore.execute(plan.slug);
		}
	}

	async function handleRefreshPlan() {
		if (slug) {
			await plansStore.fetch();
		}
	}

	async function handleRefreshRequirements() {
		await fetchRequirements();
	}

	async function handleRefreshScenarios(reqId: string) {
		await fetchScenariosForReq(reqId);
	}

	async function handleDeleteRequirement(reqId: string) {
		if (!slug) return;
		await api.requirements.delete(slug, reqId);
		await fetchRequirements();
		// Clear scenarios cache
		const { [reqId]: _, ...rest } = scenariosByReq;
		scenariosByReq = rest;
		// Navigate back to plan
		planSelectionStore.selectPlan(slug);
	}

	async function handleExpandRequirement(reqId: string) {
		if (!scenariosByReq[reqId]) {
			await fetchScenariosForReq(reqId);
		}
	}

	// Legacy handlers for phases/tasks
	async function handleRefreshTasks() {
		if (slug) {
			tasks = await plansStore.fetchTasks(slug);
		}
	}

	async function handleRefreshPhases() {
		if (slug) {
			try {
				phases = await api.phases.list(slug);
			} catch {
				// ignore
			}
		}
	}

	async function handleApprovePhase(phaseId: string) {
		if (!slug) return;
		await api.phases.approve(slug, phaseId);
		await handleRefreshPhases();
	}

	async function handleRejectPhase(phaseId: string, reason: string) {
		if (!slug) return;
		await api.phases.reject(slug, phaseId, reason);
		await handleRefreshPhases();
	}

	async function handleApproveTask(taskId: string) {
		if (!slug) return;
		const updated = await api.tasks.approve(slug, taskId);
		const index = tasks.findIndex((t) => t.id === taskId);
		if (index !== -1) {
			tasks[index] = updated;
			tasks = [...tasks];
		}
	}

	async function handleRejectTask(taskId: string, reason: string) {
		if (!slug) return;
		const updated = await api.tasks.reject(slug, taskId, reason);
		const index = tasks.findIndex((t) => t.id === taskId);
		if (index !== -1) {
			tasks[index] = updated;
			tasks = [...tasks];
		}
	}

	function getStageLabel(stage: PlanStage): string {
		switch (stage) {
			case 'draft':
			case 'drafting':
				return 'Draft';
			case 'ready_for_approval':
				return 'Ready for Approval';
			case 'reviewed':
				return 'Reviewed';
			case 'needs_changes':
				return 'Needs Changes';
			case 'planning':
				return 'Planning';
			case 'approved':
				return 'Approved';
			case 'rejected':
				return 'Rejected';
			case 'requirements_generated':
				return 'Requirements Generated';
			case 'scenarios_generated':
				return 'Scenarios Generated';
			case 'ready_for_execution':
				return 'Ready to Execute';
			case 'phases_generated':
				return 'Phases Generated';
			case 'phases_approved':
				return 'Phases Approved';
			case 'tasks_generated':
				return 'Tasks Generated';
			case 'tasks_approved':
			case 'tasks':
				return 'Ready to Execute';
			case 'implementing':
			case 'executing':
				return 'Executing';
			case 'complete':
				return 'Complete';
			case 'archived':
				return 'Archived';
			case 'failed':
				return 'Failed';
			default:
				return stage;
		}
	}
</script>

<svelte:head>
	<title>{plan?.slug || 'Plan'} - Semspec</title>
</svelte:head>

<div class="plan-detail">
	<header class="detail-header">
		<div class="header-left">
			<a href="/plans" class="back-link">
				<Icon name="chevron-left" size={16} />
				Back to Plans
			</a>
		</div>
		<div class="header-center">
			{#if plan}
				<h1 class="plan-title">{plan.title || plan.slug}</h1>
				<div class="plan-meta">
					<ModeIndicator approved={plan.approved} />
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
			{/if}
		</div>
		<div class="header-right"></div>
	</header>

	{#if !plan}
		<div class="not-found">
			<Icon name="alert-circle" size={48} />
			<h2>Plan not found</h2>
			<p>The plan "{slug}" could not be found.</p>
			<a href="/plans" class="btn btn-primary">Back to Plans</a>
		</div>
	{:else}
		<!-- Mobile tab switcher -->
		<div class="mobile-tabs">
			<button
				class="tab-btn"
				class:active={activeTab === 'nav'}
				onclick={() => (activeTab = 'nav')}
			>
				<Icon name="list" size={14} />
				Navigation
			</button>
			<button
				class="tab-btn"
				class:active={activeTab === 'detail'}
				onclick={() => (activeTab = 'detail')}
			>
				<Icon name="file-text" size={14} />
				Details
			</button>
		</div>

		<div class="workspace-layout">
			<ThreePanelLayout
				id="plan-detail"
				leftOpen={true}
				rightOpen={rightPanelShouldOpen}
				leftWidth={260}
				rightWidth={360}
			>
				{#snippet leftPanel()}
					<PlanNavTree
						{plan}
						{requirements}
						{scenariosByReq}
						selection={planSelectionStore.selection}
						onSelect={handleSelect}
						onExpandRequirement={handleExpandRequirement}
					/>
				{/snippet}

				{#snippet centerPanel()}
					<div class="center-content">
						{#if pipeline && plan.approved}
							<div class="pipeline-section">
								<PipelineIndicator
									plan={pipeline.plan}
									requirements={pipeline.requirements}
									execute={pipeline.execute}
								/>
								{#if plan.active_loops && plan.active_loops.length > 0}
									<div class="agent-pipeline-section">
										<AgentPipelineView slug={plan.slug} loops={plan.active_loops} />
									</div>
								{/if}
							</div>
						{/if}

						{#if activeRejection}
							<RejectionBanner
								rejection={activeRejection.rejection}
								taskDescription={activeRejection.task.description}
							/>
						{/if}

						{#if planQuestions.length > 0}
							<div class="questions-container">
								<QuestionQueue questions={planQuestions} onAnswer={handleAnswerQuestion} />
							</div>
						{/if}

						<ActionBar
							{plan}
							onPromote={handlePromote}
							onExecute={handleExecute}
						/>

						<div class="detail-panel">
							<PlanDetailPanel
								selection={planSelectionStore.selection}
								{plan}
								{phases}
								{tasksByPhase}
								{requirements}
								{scenariosByReq}
								onRefreshPlan={handleRefreshPlan}
								onRefreshPhases={handleRefreshPhases}
								onRefreshTasks={handleRefreshTasks}
								onRefreshRequirements={handleRefreshRequirements}
								onRefreshScenarios={handleRefreshScenarios}
								onDeleteRequirement={handleDeleteRequirement}
								onApprovePhase={handleApprovePhase}
								onRejectPhase={handleRejectPhase}
								onApproveTask={handleApproveTask}
								onRejectTask={handleRejectTask}
							/>
						</div>
					</div>
				{/snippet}

				{#snippet rightPanel()}
					<div class="right-panel-content">
						{#if activeLoopId}
							<div class="right-panel-section">
								<h3 class="right-panel-heading">
									<Icon name="activity" size={14} />
									Active Loop
								</h3>
								<TrajectoryPanel loopId={activeLoopId} compact={true} />
							</div>
						{/if}

						{#if canShowReviews}
							<div class="right-panel-section">
								<h3 class="right-panel-heading">
									<Icon name="check-circle" size={14} />
									Review Results
								</h3>
								<ReviewDashboard slug={plan.slug} />
							</div>
						{/if}

						{#if !activeLoopId && !canShowReviews}
							<div class="right-panel-empty">
								<Icon name="info" size={32} />
								<p>Context details will appear here when the plan is executing.</p>
							</div>
						{/if}
					</div>
				{/snippet}
			</ThreePanelLayout>
		</div>
	{/if}
</div>

<style>
	.plan-detail {
		padding: var(--space-4);
		max-width: 1800px;
		margin: 0 auto;
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.detail-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		margin-bottom: var(--space-4);
		gap: var(--space-4);
		flex-shrink: 0;
	}

	.header-left,
	.header-right {
		flex-shrink: 0;
		min-width: 150px;
	}

	.header-center {
		flex: 1;
		text-align: center;
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

	.plan-title {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0 0 var(--space-1);
	}

	.plan-meta {
		display: flex;
		align-items: center;
		justify-content: center;
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

	.plan-stage[data-stage='implementing'],
	.plan-stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='requirements_generated'],
	.plan-stage[data-stage='scenarios_generated'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='ready_for_execution'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='needs_changes'],
	.plan-stage[data-stage='rejected'] {
		background: var(--color-warning-muted, rgba(234, 179, 8, 0.15));
		color: var(--color-warning, #eab308);
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

	/* Mobile tabs - hidden on desktop */
	.mobile-tabs {
		display: none;
		gap: var(--space-2);
		margin-bottom: var(--space-2);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		flex-shrink: 0;
	}

	.tab-btn {
		flex: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.tab-btn:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}

	.tab-btn.active {
		color: var(--color-accent);
		background: var(--color-accent-muted);
	}

	/* Workspace layout — fills remaining vertical space */
	.workspace-layout {
		flex: 1;
		min-height: 0;
		border-top: 1px solid var(--color-border);
	}

	/* Center panel content */
	.center-content {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: auto;
		padding: var(--space-4);
	}

	.pipeline-section {
		padding-bottom: var(--space-4);
		margin-bottom: var(--space-4);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.agent-pipeline-section {
		margin-top: var(--space-4);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.questions-container {
		margin-bottom: var(--space-4);
		flex-shrink: 0;
	}

	.detail-panel {
		flex: 1;
		min-height: 0;
	}

	/* Right panel content */
	.right-panel-content {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: auto;
	}

	.right-panel-section {
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.right-panel-heading {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		margin: 0 0 var(--space-3);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.right-panel-empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-8) var(--space-4);
		color: var(--color-text-muted);
		text-align: center;
		flex: 1;
	}

	.right-panel-empty p {
		font-size: var(--font-size-sm);
		margin: 0;
	}

	/* Responsive: mobile */
	@media (max-width: 900px) {
		.mobile-tabs {
			display: flex;
		}

		.detail-header {
			flex-direction: column;
			align-items: flex-start;
			gap: var(--space-2);
		}

		.header-center {
			text-align: left;
		}

		.plan-meta {
			justify-content: flex-start;
		}
	}

	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		text-decoration: none;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover {
		background: var(--color-accent-hover);
	}
</style>
