<script lang="ts">
	import { page } from '$app/stores';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ResizableSplit from '$lib/components/shared/ResizableSplit.svelte';
	import ResizableSplitVertical from '$lib/components/shared/ResizableSplitVertical.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import PlanNavTree from '$lib/components/plan/PlanNavTree.svelte';
	import PlanDetailPanel from '$lib/components/plan/PlanDetailPanel.svelte';
	import RejectionBanner from '$lib/components/plan/RejectionBanner.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import { ReviewDashboard } from '$lib/components/review';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { planSelectionStore, type PlanSelection } from '$lib/stores/planSelection.svelte';
	import { api } from '$lib/api/client';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import type { Phase } from '$lib/types/phase';
	import { onMount } from 'svelte';

	const slug = $derived($page.params.slug);
	const plan = $derived(slug ? plansStore.getBySlug(slug) : undefined);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);

	let tasks = $state<Task[]>([]);
	let phases = $state<Phase[]>([]);
	let showReviews = $state(false);
	let activeTab = $state<'nav' | 'detail'>('nav');

	// Group tasks by phase ID for nav tree
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

	// Update label cache when plan/phases/tasks change
	$effect(() => {
		if (plan) {
			planSelectionStore.setLabel(`plan:${plan.slug}`, plan.title || plan.slug);
		}
		for (const phase of phases) {
			planSelectionStore.setLabel(`phase:${phase.id}`, phase.name);
		}
		for (const task of tasks) {
			// Use short description for label
			const label = task.description.length > 30
				? task.description.slice(0, 30) + '...'
				: task.description;
			planSelectionStore.setLabel(`task:${task.id}`, label);
		}
	});

	// Show reviews section when plan is executing or complete
	const canShowReviews = $derived(
		plan?.approved &&
			(plan?.stage === 'implementing' ||
				plan?.stage === 'executing' ||
				plan?.stage === 'complete')
	);

	onMount(() => {
		// Initial data fetch
		plansStore.fetch().then(() => {
			if (slug) {
				// Fetch both tasks and phases
				plansStore.fetchTasks(slug).then((fetched) => {
					tasks = fetched;
				});
				api.phases.list(slug).then((fetched) => {
					phases = fetched;
				}).catch((err) => {
					console.warn('Failed to fetch phases:', err);
					phases = [];
				});
			}
		});
		// Fetch questions for QuestionQueue
		questionsStore.fetch('pending');
		const interval = setInterval(() => questionsStore.fetch('pending'), 10000);

		// Clear selection on unmount
		return () => {
			clearInterval(interval);
			planSelectionStore.clear();
		};
	});

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

	// Handle "Answer" click - opens drawer with question context
	function handleAnswerQuestion(questionId: string): void {
		chatDrawerStore.open({ type: 'question', questionId, planSlug: slug });
	}

	// Find any task with an active rejection
	const activeRejection = $derived.by(() => {
		const rejectedTask = tasks.find((t) => t.rejection && t.status === 'in_progress');
		return rejectedTask ? { task: rejectedTask, rejection: rejectedTask.rejection! } : null;
	});

	// Selection handler
	function handleSelect(selection: PlanSelection): void {
		planSelectionStore.selection = selection;
	}

	// Get label for a selection (for chat context)
	function getContextLabel(selection: PlanSelection): string {
		return planSelectionStore.getLabel(selection);
	}

	// Action handlers
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

	async function handleRefreshPlan() {
		if (slug) {
			await plansStore.fetch();
		}
	}

	async function handleRefreshTasks() {
		if (slug) {
			tasks = await plansStore.fetchTasks(slug);
		}
	}

	async function handleRefreshPhases() {
		if (slug) {
			try {
				phases = await api.phases.list(slug);
			} catch (err) {
				console.warn('Failed to fetch phases:', err);
			}
		}
	}

	async function handleGeneratePhases() {
		if (slug) {
			try {
				const generated = await api.phases.generate(slug);
				phases = generated;
			} catch (err) {
				console.error('Failed to generate phases:', err);
			}
		}
	}

	// Phase approval handlers
	async function handleApprovePhase(phaseId: string) {
		if (!slug) return;
		try {
			await api.phases.approve(slug, phaseId);
			await handleRefreshPhases();
		} catch (err) {
			console.error('Failed to approve phase:', err);
		}
	}

	async function handleRejectPhase(phaseId: string, reason: string) {
		if (!slug) return;
		try {
			await api.phases.reject(slug, phaseId, reason);
			await handleRefreshPhases();
		} catch (err) {
			console.error('Failed to reject phase:', err);
		}
	}

	// Task approval handlers
	async function handleApproveTask(taskId: string) {
		if (!slug) return;
		try {
			const updated = await api.tasks.approve(slug, taskId);
			const index = tasks.findIndex((t) => t.id === taskId);
			if (index !== -1) {
				tasks[index] = updated;
				tasks = [...tasks];
			}
		} catch (err) {
			console.error('Failed to approve task:', err);
		}
	}

	async function handleRejectTask(taskId: string, reason: string) {
		if (!slug) return;
		try {
			const updated = await api.tasks.reject(slug, taskId, reason);
			const index = tasks.findIndex((t) => t.id === taskId);
			if (index !== -1) {
				tasks[index] = updated;
				tasks = [...tasks];
			}
		} catch (err) {
			console.error('Failed to reject task:', err);
		}
	}

	async function handleApproveAllTasks() {
		if (!slug) return;
		try {
			await api.plans.approveTasks(slug);
			tasks = await plansStore.fetchTasks(slug);
		} catch (err) {
			console.error('Failed to approve all tasks:', err);
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
		{#if pipeline && plan.approved}
			<div class="pipeline-section">
				<PipelineIndicator
					plan={pipeline.plan}
					tasks={pipeline.tasks}
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

		<!-- Questions Queue - inline above panels -->
		{#if planQuestions.length > 0}
			<div class="questions-container">
				<QuestionQueue questions={planQuestions} onAnswer={handleAnswerQuestion} />
			</div>
		{/if}

		<ActionBar
			{plan}
			{tasks}
			{phases}
			onPromote={handlePromote}
			onGenerateTasks={handleGenerateTasks}
			onGeneratePhases={handleGeneratePhases}
			onApproveAll={handleApproveAllTasks}
			onExecute={handleExecute}
		/>

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

		<div class="workspace-layout" class:mobile-nav={activeTab === 'nav'} class:mobile-detail={activeTab === 'detail'}>
			<ResizableSplit
				id="plan-workspace"
				defaultRatio={0.25}
				minLeftWidth={200}
				minRightWidth={400}
				leftTitle="Plan Structure"
			>
				{#snippet left()}
					<PlanNavTree
						{plan}
						{phases}
						{tasksByPhase}
						selection={planSelectionStore.selection}
						onSelect={handleSelect}
					/>
				{/snippet}

				{#snippet right()}
					<ResizableSplitVertical
						id="plan-detail-chat"
						defaultRatio={0.6}
						minTopHeight={200}
						minBottomHeight={150}
					>
						{#snippet top()}
							<PlanDetailPanel
								selection={planSelectionStore.selection}
								{plan}
								{phases}
								{tasksByPhase}
								onRefreshPlan={handleRefreshPlan}
								onRefreshPhases={handleRefreshPhases}
								onRefreshTasks={handleRefreshTasks}
								onApprovePhase={handleApprovePhase}
								onRejectPhase={handleRejectPhase}
								onApproveTask={handleApproveTask}
								onRejectTask={handleRejectTask}
							/>
						{/snippet}

						{#snippet bottom()}
							<ChatPanel
								title="Chat"
								planSlug={slug}
								selectionContext={planSelectionStore.selection}
								{getContextLabel}
							/>
						{/snippet}
					</ResizableSplitVertical>
				{/snippet}
			</ResizableSplit>

			{#if canShowReviews}
				<div class="reviews-section">
					<button
						class="reviews-toggle"
						onclick={() => (showReviews = !showReviews)}
						aria-expanded={showReviews}
					>
						<Icon name={showReviews ? 'chevron-down' : 'chevron-right'} size={16} />
						<span>Review Results</span>
					</button>

					{#if showReviews}
						<div class="reviews-content">
							<ReviewDashboard slug={plan.slug} />
						</div>
					{/if}
				</div>
			{/if}
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

	.questions-container {
		margin-bottom: var(--space-4);
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

	.pipeline-section {
		padding: var(--space-4) 0;
		margin-bottom: var(--space-4);
	}

	.agent-pipeline-section {
		margin-top: var(--space-4);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	/* Mobile tabs - hidden on desktop */
	.mobile-tabs {
		display: none;
		gap: var(--space-2);
		margin-bottom: var(--space-4);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
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

	/* Workspace layout */
	.workspace-layout {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	/* Responsive: mobile - tabbed interface */
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

		.workspace-layout.mobile-nav :global(.panel-right) {
			display: none;
		}

		.workspace-layout.mobile-detail :global(.panel-left) {
			display: none;
		}
	}

	.reviews-section {
		margin-top: var(--space-6);
		padding-top: var(--space-6);
		border-top: 1px solid var(--color-border);
	}

	.reviews-toggle {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.reviews-toggle:hover {
		background: var(--color-bg-elevated);
		border-color: var(--color-accent);
	}

	.reviews-content {
		margin-top: var(--space-4);
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
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
