<script lang="ts">
	import { page } from '$app/stores';
	import Icon from '$lib/components/shared/Icon.svelte';
	import ResizableSplit from '$lib/components/shared/ResizableSplit.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PlanPanel from '$lib/components/plan/PlanPanel.svelte';
	import TaskList from '$lib/components/plan/TaskList.svelte';
	import RejectionBanner from '$lib/components/plan/RejectionBanner.svelte';
	import ActionBar from '$lib/components/plan/ActionBar.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import { ReviewDashboard } from '$lib/components/review';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import ChatDrawerTrigger from '$lib/components/chat/ChatDrawerTrigger.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { questionsStore } from '$lib/stores/questions.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import { api } from '$lib/api/client';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import { onMount } from 'svelte';

	const slug = $derived($page.params.slug);
	const plan = $derived(slug ? plansStore.getBySlug(slug) : undefined);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);

	let tasks = $state<Task[]>([]);
	let showReviews = $state(false);
	let activeTab = $state<'plan' | 'tasks'>('plan');

	// Show reviews section when plan is executing or complete
	const canShowReviews = $derived(
		plan?.approved && (plan?.stage === 'executing' || plan?.stage === 'complete')
	);

	onMount(() => {
		// Initial data fetch
		plansStore.fetch().then(() => {
			if (slug) {
				plansStore.fetchTasks(slug).then((fetched) => {
					tasks = fetched;
				});
			}
		});
		// Fetch questions for QuestionQueue
		questionsStore.fetch('pending');
		const interval = setInterval(() => questionsStore.fetch('pending'), 10000);
		return () => clearInterval(interval);
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

	// Task approval handlers
	async function handleApproveTask(taskId: string) {
		if (!slug) return;
		try {
			const updated = await api.tasks.approve(slug, taskId);
			// Update local tasks array
			const index = tasks.findIndex((t) => t.id === taskId);
			if (index !== -1) {
				tasks[index] = updated;
				tasks = [...tasks]; // Trigger reactivity
			}
		} catch (err) {
			console.error('Failed to approve task:', err);
		}
	}

	async function handleRejectTask(taskId: string, reason: string) {
		if (!slug) return;
		try {
			const updated = await api.tasks.reject(slug, taskId, reason);
			// Update local tasks array
			const index = tasks.findIndex((t) => t.id === taskId);
			if (index !== -1) {
				tasks[index] = updated;
				tasks = [...tasks]; // Trigger reactivity
			}
		} catch (err) {
			console.error('Failed to reject task:', err);
		}
	}

	async function handleApproveAllTasks() {
		if (!slug) return;
		try {
			await api.plans.approveTasks(slug);
			// Refresh tasks after bulk approval
			tasks = await plansStore.fetchTasks(slug);
		} catch (err) {
			console.error('Failed to approve all tasks:', err);
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

	// Determine if user can add tasks (plan approved but not yet complete/executing)
	const canAddTask = $derived(
		plan?.approved &&
			!['executing', 'complete', 'failed', 'archived'].includes(plan?.stage ?? '')
	);

	async function handleDeleteTask(taskId: string) {
		if (!slug) return;
		try {
			await api.tasks.delete(slug, taskId);
			// Remove from local tasks array
			tasks = tasks.filter((t) => t.id !== taskId);
		} catch (err) {
			console.error('Failed to delete task:', err);
		}
	}

	// Computed values for task stats
	const pendingApprovalCount = $derived(
		tasks.filter((t) => t.status === 'pending_approval').length
	);
	const approvedCount = $derived(tasks.filter((t) => t.status === 'approved').length);
	const allTasksApproved = $derived(
		tasks.length > 0 && tasks.every((t) => t.status === 'approved' || t.status === 'completed')
	);

	function getStageLabel(stage: PlanStage): string {
		switch (stage) {
			case 'draft':
			case 'drafting':
				return 'Draft';
			case 'ready_for_approval':
				return 'Ready for Approval';
			case 'planning':
				return 'Planning';
			case 'approved':
				return 'Approved';
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
		<div class="header-right">
			{#if plan}
				<ChatDrawerTrigger
					context={{ type: 'plan', planSlug: plan.slug }}
					variant="icon"
				/>
			{/if}
		</div>
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
		</div>

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
			onPromote={handlePromote}
			onGenerateTasks={handleGenerateTasks}
			onApproveAll={handleApproveAllTasks}
			onExecute={handleExecute}
		/>

		<!-- Mobile tab switcher -->
		<div class="mobile-tabs">
			<button
				class="tab-btn"
				class:active={activeTab === 'plan'}
				onclick={() => (activeTab = 'plan')}
			>
				<Icon name="file-text" size={14} />
				Plan
			</button>
			<button
				class="tab-btn"
				class:active={activeTab === 'tasks'}
				onclick={() => (activeTab = 'tasks')}
			>
				<Icon name="list" size={14} />
				Tasks
			</button>
		</div>

		<div class="panel-layout" class:hidden-mobile-plan={activeTab !== 'plan'} class:hidden-mobile-tasks={activeTab !== 'tasks'}>
			<ResizableSplit
				id="plan-detail"
				defaultRatio={0.5}
				minLeftWidth={250}
				minRightWidth={300}
				leftTitle="Plan"
				rightTitle="Tasks"
			>
				{#snippet left()}
					<PlanPanel {plan} onRefresh={handleRefreshPlan} />
				{/snippet}

				{#snippet right()}
					<TaskList
						{tasks}
						planSlug={plan.slug}
						activeLoops={plan.active_loops}
						{canAddTask}
						onApprove={handleApproveTask}
						onReject={handleRejectTask}
						onDelete={handleDeleteTask}
						onApproveAll={handleApproveAllTasks}
						onTasksChange={handleRefreshTasks}
					/>
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
				{/snippet}
			</ResizableSplit>
		</div>
	{/if}
</div>

<style>
	.plan-detail {
		padding: var(--space-6);
		max-width: 1600px;
		margin: 0 auto;
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.detail-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.header-left {
		display: flex;
		align-items: center;
	}

	.header-right {
		display: flex;
		align-items: center;
		gap: var(--space-2);
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

	/* Panel layout - resizable split view */
	.panel-layout {
		display: flex;
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

		/* On mobile, show plan or tasks based on tab */
		.hidden-mobile-tasks :global(.panel-right) {
			display: none;
		}

		.hidden-mobile-plan :global(.panel-left) {
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
</style>
