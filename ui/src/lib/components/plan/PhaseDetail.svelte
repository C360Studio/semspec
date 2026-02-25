<script lang="ts">
	/**
	 * PhaseDetail - Detail view for a selected phase.
	 *
	 * Shows phase name, description, dependencies, and workflow guidance
	 * based on the current phase status.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import { api } from '$lib/api/client';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Phase, PhaseStatus } from '$lib/types/phase';
	import type { Task } from '$lib/types/task';

	interface Props {
		phase: Phase;
		plan: PlanWithStatus;
		tasks: Task[];
		allPhases: Phase[];
		onRefresh?: () => Promise<void>;
		onRefreshTasks?: () => Promise<void>;
		onApprove?: (phaseId: string) => Promise<void>;
		onReject?: (phaseId: string, reason: string) => Promise<void>;
	}

	let {
		phase,
		plan,
		tasks,
		allPhases,
		onRefresh,
		onRefreshTasks,
		onApprove,
		onReject
	}: Props = $props();

	let approving = $state(false);
	let rejecting = $state(false);
	let generatingTasks = $state(false);
	let rejectReason = $state('');
	let showRejectForm = $state(false);
	let error = $state<string | null>(null);

	// Find dependency phase names
	const dependencyNames = $derived.by(() => {
		if (!phase.depends_on || phase.depends_on.length === 0) return [];
		return phase.depends_on
			.map((depId) => {
				const dep = allPhases.find((p) => p.id === depId);
				return dep?.name ?? depId;
			});
	});

	// Task stats
	const taskStats = $derived.by(() => {
		const total = tasks.length;
		const completed = tasks.filter((t) => t.status === 'completed').length;
		const inProgress = tasks.filter((t) => t.status === 'in_progress').length;
		const pending = tasks.filter(
			(t) => t.status === 'pending' || t.status === 'pending_approval' || t.status === 'approved'
		).length;
		return { total, completed, inProgress, pending };
	});

	// Workflow guidance based on phase status
	const guidance = $derived.by(() => {
		if (phase.requires_approval && !phase.approved) {
			return {
				message: 'Approve this phase to generate tasks.',
				showApprove: true,
				showReject: true,
				showGenerateTasks: false
			};
		}

		if (phase.approved && tasks.length === 0) {
			return {
				message: 'Generating tasks for this phase...',
				showApprove: false,
				showReject: false,
				showGenerateTasks: true,
				isLoading: generatingTasks
			};
		}

		if (tasks.length > 0) {
			const unapprovedTasks = tasks.filter((t) => t.status === 'pending_approval');
			if (unapprovedTasks.length > 0) {
				return {
					message: `Review and approve ${unapprovedTasks.length} task${unapprovedTasks.length > 1 ? 's' : ''}.`,
					showApprove: false,
					showReject: false,
					showGenerateTasks: false
				};
			}

			if (taskStats.completed === taskStats.total) {
				return {
					message: 'All tasks completed.',
					showApprove: false,
					showReject: false,
					showGenerateTasks: false
				};
			}

			return {
				message: 'Select a task to view details.',
				showApprove: false,
				showReject: false,
				showGenerateTasks: false
			};
		}

		return {
			message: '',
			showApprove: false,
			showReject: false,
			showGenerateTasks: false
		};
	});

	function getStatusForBadge(status: PhaseStatus): string {
		switch (status) {
			case 'complete':
				return 'completed';
			case 'active':
				return 'in_progress';
			case 'failed':
				return 'failed';
			case 'blocked':
				return 'blocked';
			case 'ready':
				return 'ready';
			default:
				return 'pending';
		}
	}

	async function handleApprove(): Promise<void> {
		approving = true;
		error = null;
		try {
			await onApprove?.(phase.id);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to approve phase';
		} finally {
			approving = false;
		}
	}

	async function handleReject(): Promise<void> {
		if (!rejectReason.trim()) {
			error = 'Please provide a reason for rejection';
			return;
		}
		rejecting = true;
		error = null;
		try {
			await onReject?.(phase.id, rejectReason);
			showRejectForm = false;
			rejectReason = '';
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to reject phase';
		} finally {
			rejecting = false;
		}
	}

	async function handleGenerateTasks(): Promise<void> {
		generatingTasks = true;
		error = null;
		try {
			// Generate tasks for the plan (API generates for approved phases)
			await api.plans.generateTasks(plan.slug);
			await onRefreshTasks?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to generate tasks';
		} finally {
			generatingTasks = false;
		}
	}
</script>

<div class="phase-detail">
	<!-- Header -->
	<header class="detail-header">
		<div class="header-main">
			<div class="header-breadcrumb">
				<span class="breadcrumb-item">{plan.title || plan.slug}</span>
				<Icon name="chevron-right" size={12} />
			</div>
			<h2 class="detail-title">{phase.name}</h2>
			<StatusBadge status={getStatusForBadge(phase.status as PhaseStatus)} />
		</div>
	</header>

	{#if error}
		<div class="error-message" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
		</div>
	{/if}

	<!-- Content -->
	<div class="detail-content">
		{#if phase.description}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="file-text" size={14} />
					Description
				</dt>
				<dd class="section-content">{phase.description}</dd>
			</div>
		{/if}

		{#if dependencyNames.length > 0}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="git-branch" size={14} />
					Dependencies
				</dt>
				<dd class="dependencies-list">
					{#each dependencyNames as depName}
						<span class="dependency-tag">{depName}</span>
					{/each}
				</dd>
			</div>
		{/if}

		{#if tasks.length > 0}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="check-square" size={14} />
					Tasks
				</dt>
				<dd class="task-stats">
					<span class="stat">
						<span class="stat-value">{taskStats.total}</span>
						<span class="stat-label">Total</span>
					</span>
					<span class="stat">
						<span class="stat-value completed">{taskStats.completed}</span>
						<span class="stat-label">Completed</span>
					</span>
					{#if taskStats.inProgress > 0}
						<span class="stat">
							<span class="stat-value active">{taskStats.inProgress}</span>
							<span class="stat-label">In Progress</span>
						</span>
					{/if}
					{#if taskStats.pending > 0}
						<span class="stat">
							<span class="stat-value pending">{taskStats.pending}</span>
							<span class="stat-label">Pending</span>
						</span>
					{/if}
				</dd>
			</div>
		{/if}
	</div>

	<!-- Reject form -->
	{#if showRejectForm}
		<div class="reject-form">
			<label class="section-label" for="reject-reason">
				<Icon name="x-circle" size={14} />
				Rejection Reason
			</label>
			<textarea
				id="reject-reason"
				class="reject-textarea"
				bind:value={rejectReason}
				placeholder="Explain why this phase should be revised..."
				rows="3"
			></textarea>
			<div class="reject-actions">
				<button
					class="btn btn-ghost btn-sm"
					onclick={() => {
						showRejectForm = false;
						rejectReason = '';
					}}
					disabled={rejecting}
				>
					Cancel
				</button>
				<button
					class="btn btn-error btn-sm"
					onclick={handleReject}
					disabled={rejecting || !rejectReason.trim()}
				>
					{rejecting ? 'Rejecting...' : 'Confirm Rejection'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Workflow Guidance -->
	{#if guidance.message && !showRejectForm}
		<div class="detail-guidance">
			<div class="guidance-hint">
				<Icon name="lightbulb" size={14} />
				<span>{guidance.message}</span>
			</div>
			<div class="guidance-actions">
				{#if guidance.showApprove}
					<button
						class="btn btn-primary"
						onclick={handleApprove}
						disabled={approving}
					>
						{#if approving}
							<Icon name="loader" size={14} />
							Approving...
						{:else}
							<Icon name="check" size={14} />
							Approve Phase
						{/if}
					</button>
				{/if}
				{#if guidance.showReject}
					<button
						class="btn btn-ghost"
						onclick={() => (showRejectForm = true)}
					>
						<Icon name="x" size={14} />
						Reject
					</button>
				{/if}
				{#if guidance.showGenerateTasks}
					<button
						class="btn btn-primary"
						onclick={handleGenerateTasks}
						disabled={generatingTasks}
					>
						{#if generatingTasks}
							<Icon name="loader" size={14} />
							Generating...
						{:else}
							<Icon name="zap" size={14} />
							Generate Tasks
						{/if}
					</button>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.phase-detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-4);
	}

	.detail-header {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.header-main {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.header-breadcrumb {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.detail-title {
		margin: 0;
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.error-message {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	.detail-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.detail-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-accent);
		margin: 0;
	}

	.section-content {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-relaxed);
		color: var(--color-text-primary);
		white-space: pre-wrap;
	}

	/* Dependencies */
	.dependencies-list {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: 0;
	}

	.dependency-tag {
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	/* Task stats */
	.task-stats {
		display: flex;
		gap: var(--space-4);
		margin: 0;
	}

	.stat {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1);
	}

	.stat-value {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.stat-value.completed {
		color: var(--color-success);
	}

	.stat-value.active {
		color: var(--color-accent);
	}

	.stat-value.pending {
		color: var(--color-warning);
	}

	.stat-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	/* Reject form */
	.reject-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.05));
		border: 1px solid var(--color-error);
		border-radius: var(--radius-md);
	}

	.reject-textarea {
		width: 100%;
		padding: var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-family: inherit;
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		resize: vertical;
	}

	.reject-textarea:focus {
		outline: none;
		border-color: var(--color-error);
	}

	.reject-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
	}

	/* Workflow guidance */
	.detail-guidance {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
	}

	.guidance-hint {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.guidance-hint :global(svg) {
		flex-shrink: 0;
		margin-top: 2px;
		color: var(--color-warning);
	}

	.guidance-actions {
		display: flex;
		gap: var(--space-2);
	}

	/* Buttons */
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
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.btn-sm {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.btn-ghost {
		background: transparent;
		color: var(--color-text-secondary);
		border: 1px solid var(--color-border);
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-error {
		background: var(--color-error);
		color: white;
	}

	.btn-error:hover:not(:disabled) {
		background: var(--color-error-hover, #dc2626);
	}
</style>
