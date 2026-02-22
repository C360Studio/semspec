<script lang="ts">
	import { page } from '$app/stores';
	import Icon from '$lib/components/shared/Icon.svelte';
	import CollapsiblePanel from '$lib/components/shared/CollapsiblePanel.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PlanPanel from '$lib/components/plan/PlanPanel.svelte';
	import PlanChat from '$lib/components/plan/PlanChat.svelte';
	import TaskList from '$lib/components/plan/TaskList.svelte';
	import RejectionBanner from '$lib/components/plan/RejectionBanner.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { AgentPipelineView } from '$lib/components/pipeline';
	import { ReviewDashboard } from '$lib/components/review';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { api } from '$lib/api/client';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import { onMount } from 'svelte';

	const slug = $derived($page.params.slug);
	const plan = $derived(slug ? plansStore.getBySlug(slug) : undefined);
	const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);

	let tasks = $state<Task[]>([]);
	let showReviews = $state(false);
	let activeTab = $state<'plan' | 'chat'>('plan');

	// Show reviews section when plan is executing or complete
	const canShowReviews = $derived(
		plan?.approved && (plan?.stage === 'executing' || plan?.stage === 'complete')
	);

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

	function handleEditTask(task: Task) {
		// TODO: Open task edit modal
		console.log('Edit task:', task);
	}

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

		{#if !plan.approved && plan.goal}
			<div class="action-banner promote">
				<Icon name="arrow-up" size={20} />
				<div class="action-content">
					<strong>Ready to approve</strong>
					<p>This draft plan has enough context. Approve it to generate tasks.</p>
				</div>
				<button class="btn btn-primary" onclick={handlePromote}>
					Approve Plan
				</button>
			</div>
		{/if}

		{#if plan.approved && plan.stage === 'planning'}
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

		{#if pendingApprovalCount > 0}
			<div class="action-banner review">
				<Icon name="clock" size={20} />
				<div class="action-content">
					<strong>Review {pendingApprovalCount} task{pendingApprovalCount === 1 ? '' : 's'}</strong>
					<p>Tasks are pending approval. Review and approve them before execution.</p>
				</div>
				<button class="btn btn-primary" onclick={handleApproveAllTasks}>
					Approve All
				</button>
			</div>
		{:else if allTasksApproved && (plan.stage === 'tasks' || plan.stage === 'tasks_approved' || plan.stage === 'tasks_generated')}
			<div class="action-banner execute">
				<Icon name="play" size={20} />
				<div class="action-content">
					<strong>Ready to execute</strong>
					<p>{approvedCount} task{approvedCount === 1 ? '' : 's'} approved. Start execution to begin implementation.</p>
				</div>
				<button class="btn btn-success" onclick={handleExecute}>
					Start Execution
				</button>
			</div>
		{/if}

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
				class:active={activeTab === 'chat'}
				onclick={() => (activeTab = 'chat')}
			>
				<Icon name="message-square" size={14} />
				Chat
			</button>
		</div>

		<div class="panel-layout" class:hidden-mobile-plan={activeTab !== 'plan'} class:hidden-mobile-chat={activeTab !== 'chat'}>
			<!-- Plan details panel (collapsible) -->
			<CollapsiblePanel id="plan-detail-plan" title="Plan" width="300px" minWidth="250px">
				<div class="panel-body">
					<PlanPanel {plan} />
				</div>
			</CollapsiblePanel>

			<!-- Tasks panel (main content, flexible) -->
			<CollapsiblePanel id="plan-detail-tasks" title="Tasks" flex={true}>
				<div class="panel-body">
					<TaskList
						{tasks}
						activeLoops={plan.active_loops}
						onApprove={handleApproveTask}
						onReject={handleRejectTask}
						onEdit={handleEditTask}
						onDelete={handleDeleteTask}
						onApproveAll={handleApproveAllTasks}
					/>
				</div>
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
			</CollapsiblePanel>

			<!-- Chat panel (collapsible) -->
			<CollapsiblePanel id="plan-detail-chat" title="Chat" width="400px" minWidth="300px">
				<PlanChat planSlug={plan.slug} />
			</CollapsiblePanel>
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

	.agent-pipeline-section {
		margin-top: var(--space-4);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
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

	.action-banner.review {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		border-color: var(--color-warning);
		color: var(--color-warning);
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

	/* Panel layout - three collapsible panels side by side */
	.panel-layout {
		display: flex;
		gap: var(--space-4);
		flex: 1;
		min-height: 0;
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.panel-body {
		padding: var(--space-4);
		height: 100%;
		overflow: auto;
	}

	/* Responsive: tablet - reduce gaps */
	@media (max-width: 1200px) {
		.panel-layout {
			gap: var(--space-3);
		}
	}

	/* Responsive: mobile - tabbed interface */
	@media (max-width: 900px) {
		.mobile-tabs {
			display: flex;
		}

		.panel-layout {
			flex-direction: column;
		}

		/* On mobile, show plan+tasks or chat based on tab */
		.hidden-mobile-plan :global([data-panel-id="plan-detail-chat"]) {
			display: none;
		}

		.hidden-mobile-chat :global([data-panel-id="plan-detail-plan"]),
		.hidden-mobile-chat :global([data-panel-id="plan-detail-tasks"]) {
			display: none;
		}

		/* On mobile, panels should stack and not collapse */
		.panel-layout :global(.collapsible-panel) {
			width: 100% !important;
			min-width: 100% !important;
			flex: none !important;
		}

		.panel-layout :global(.collapsible-panel.collapsed) {
			width: 100% !important;
			min-width: 100% !important;
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
