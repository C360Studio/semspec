<script lang="ts">
	/**
	 * TaskDetail - Detail view for a selected task.
	 *
	 * Shows task description, acceptance criteria, files, and workflow guidance
	 * based on the current task status.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Phase } from '$lib/types/phase';
	import type { Task, TaskStatus, getTaskStatusInfo } from '$lib/types/task';

	interface Props {
		task: Task;
		phase: Phase;
		plan: PlanWithStatus;
		onRefresh?: () => Promise<void>;
		onApprove?: (taskId: string) => Promise<void>;
		onReject?: (taskId: string, reason: string) => Promise<void>;
	}

	let { task, phase, plan, onRefresh, onApprove, onReject }: Props = $props();

	let approving = $state(false);
	let rejecting = $state(false);
	let rejectReason = $state('');
	let showRejectForm = $state(false);
	let error = $state<string | null>(null);

	// Workflow guidance based on task status
	const guidance = $derived.by(() => {
		switch (task.status) {
			case 'pending_approval':
				return {
					message: 'Review and approve this task to enable execution.',
					showApprove: true,
					showReject: true,
					showExecute: false
				};
			case 'approved':
				return {
					message: 'Task is approved and ready for execution.',
					showApprove: false,
					showReject: false,
					showExecute: true
				};
			case 'in_progress':
				return {
					message: 'Task is currently being executed.',
					showApprove: false,
					showReject: false,
					showExecute: false
				};
			case 'completed':
				return {
					message: 'Task completed successfully.',
					showApprove: false,
					showReject: false,
					showExecute: false
				};
			case 'failed':
				return {
					message: 'Task execution failed. Review and retry.',
					showApprove: false,
					showReject: false,
					showExecute: false
				};
			case 'rejected':
				return {
					message: 'Task was rejected and needs revision.',
					showApprove: false,
					showReject: false,
					showExecute: false
				};
			default:
				return {
					message: '',
					showApprove: false,
					showReject: false,
					showExecute: false
				};
		}
	});

	function getStatusForBadge(status: TaskStatus): string {
		switch (status) {
			case 'completed':
				return 'completed';
			case 'in_progress':
				return 'in_progress';
			case 'failed':
				return 'failed';
			case 'rejected':
				return 'rejected';
			case 'approved':
				return 'approved';
			case 'pending_approval':
				return 'pending_approval';
			default:
				return 'pending';
		}
	}

	async function handleApprove(): Promise<void> {
		approving = true;
		error = null;
		try {
			await onApprove?.(task.id);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to approve task';
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
			await onReject?.(task.id, rejectReason);
			showRejectForm = false;
			rejectReason = '';
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to reject task';
		} finally {
			rejecting = false;
		}
	}
</script>

<div class="task-detail">
	<!-- Header -->
	<header class="detail-header">
		<div class="header-main">
			<div class="header-breadcrumb">
				<span class="breadcrumb-item">{plan.title || plan.slug}</span>
				<Icon name="chevron-right" size={12} />
				<span class="breadcrumb-item">{phase.name}</span>
				<Icon name="chevron-right" size={12} />
			</div>
			<h2 class="detail-title">{task.description}</h2>
			<div class="header-meta">
				<StatusBadge status={getStatusForBadge(task.status)} />
				{#if task.type}
					<span class="task-type">{task.type}</span>
				{/if}
			</div>
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
		<!-- Acceptance Criteria -->
		{#if task.acceptance_criteria && task.acceptance_criteria.length > 0}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="check-square" size={14} />
					Acceptance Criteria
				</dt>
				<dd class="criteria-list">
					{#each task.acceptance_criteria as criterion, i}
						<div class="criterion">
							<span class="criterion-number">{i + 1}</span>
							<div class="criterion-content">
								{#if criterion.given}
									<div class="criterion-clause">
										<span class="clause-label">Given</span>
										<span class="clause-text">{criterion.given}</span>
									</div>
								{/if}
								{#if criterion.when}
									<div class="criterion-clause">
										<span class="clause-label">When</span>
										<span class="clause-text">{criterion.when}</span>
									</div>
								{/if}
								{#if criterion.then}
									<div class="criterion-clause">
										<span class="clause-label">Then</span>
										<span class="clause-text">{criterion.then}</span>
									</div>
								{/if}
							</div>
						</div>
					{/each}
				</dd>
			</div>
		{/if}

		<!-- Files -->
		{#if task.files && task.files.length > 0}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="file" size={14} />
					Files
				</dt>
				<dd class="files-list">
					{#each task.files as file}
						<span class="file-tag">
							<Icon name="file-text" size={12} />
							{file}
						</span>
					{/each}
				</dd>
			</div>
		{/if}

		<!-- Rejection info -->
		{#if task.rejection}
			<div class="detail-section rejection-info">
				<dt class="section-label rejection">
					<Icon name="alert-triangle" size={14} />
					Rejection
				</dt>
				<dd class="rejection-content">
					<div class="rejection-type">
						Type: <strong>{task.rejection.type}</strong>
					</div>
					<div class="rejection-reason">{task.rejection.reason}</div>
					<div class="rejection-meta">
						Iteration {task.rejection.iteration} •
						{new Date(task.rejection.rejected_at).toLocaleString()}
					</div>
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
				placeholder="Explain why this task should be revised..."
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
							Approve Task
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
				{#if guidance.showExecute}
					<span class="ready-indicator">
						<Icon name="play-circle" size={14} />
						Ready for execution
					</span>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.task-detail {
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

	.header-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.task-type {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		text-transform: capitalize;
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

	.section-label.rejection {
		color: var(--color-error);
	}

	/* Acceptance Criteria */
	.criteria-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin: 0;
	}

	.criterion {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
	}

	.criterion-number {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		flex-shrink: 0;
	}

	.criterion-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		flex: 1;
	}

	.criterion-clause {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.clause-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
		text-transform: uppercase;
	}

	.clause-text {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: var(--line-height-relaxed);
	}

	/* Files */
	.files-list {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: 0;
	}

	.file-tag {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
	}

	/* Rejection info */
	.rejection-info {
		padding: var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.05));
		border: 1px solid var(--color-error);
		border-radius: var(--radius-md);
	}

	.rejection-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0;
	}

	.rejection-type {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.rejection-reason {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: var(--line-height-relaxed);
	}

	.rejection-meta {
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
		align-items: center;
		gap: var(--space-2);
	}

	.ready-indicator {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-sm);
		color: var(--color-success);
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
