<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import type { Task } from '$lib/types/task';
	import type { ActiveLoop } from '$lib/types/plan';
	import {
		getTaskTypeLabel,
		getRejectionRouting,
		getTaskStatusInfo,
		canApproveTask,
		canEditTask,
		canDeleteTask
	} from '$lib/types/task';

	interface Props {
		tasks: Task[];
		activeLoops?: ActiveLoop[];
		showAcceptanceCriteria?: boolean;
		onApprove?: (taskId: string) => void;
		onReject?: (taskId: string, reason: string) => void;
		onEdit?: (task: Task) => void;
		onDelete?: (taskId: string) => void;
		onApproveAll?: () => void;
	}

	let {
		tasks,
		activeLoops = [],
		showAcceptanceCriteria = false,
		onApprove,
		onReject,
		onEdit,
		onDelete,
		onApproveAll
	}: Props = $props();

	const completedCount = $derived(tasks.filter((t) => t.status === 'completed').length);
	const pendingApprovalCount = $derived(tasks.filter((t) => t.status === 'pending_approval').length);
	const approvedCount = $derived(tasks.filter((t) => t.status === 'approved').length);

	// Track which tasks have expanded acceptance criteria
	let expandedTasks = $state<Set<string>>(new Set());

	// Track which task is being rejected (to show reason input)
	let rejectingTaskId = $state<string | null>(null);
	let rejectReason = $state('');

	function toggleExpand(taskId: string) {
		if (expandedTasks.has(taskId)) {
			expandedTasks.delete(taskId);
		} else {
			expandedTasks.add(taskId);
		}
		expandedTasks = new Set(expandedTasks);
	}

	function getStatusIcon(status: Task['status']): string {
		const info = getTaskStatusInfo(status);
		return info.icon;
	}

	function getLoopForTask(task: Task): ActiveLoop | undefined {
		if (!task.assigned_loop_id) return undefined;
		return activeLoops.find((l) => l.loop_id === task.assigned_loop_id);
	}

	function getTaskTypeIcon(type: Task['type']): string {
		switch (type) {
			case 'implement':
				return 'code';
			case 'test':
				return 'check-square';
			case 'document':
				return 'file-text';
			case 'review':
				return 'eye';
			case 'refactor':
				return 'refresh-cw';
			default:
				return 'box';
		}
	}

	function handleApprove(taskId: string) {
		onApprove?.(taskId);
	}

	function startReject(taskId: string) {
		rejectingTaskId = taskId;
		rejectReason = '';
	}

	function cancelReject() {
		rejectingTaskId = null;
		rejectReason = '';
	}

	function confirmReject() {
		if (rejectingTaskId && rejectReason.trim()) {
			onReject?.(rejectingTaskId, rejectReason.trim());
			rejectingTaskId = null;
			rejectReason = '';
		}
	}

	function handleApproveAll() {
		onApproveAll?.();
	}
</script>

<div class="task-list">
	<div class="task-list-header">
		<h3 class="panel-title">
			Tasks
			<span class="task-count">{completedCount}/{tasks.length}</span>
		</h3>
		{#if pendingApprovalCount > 0 && onApproveAll}
			<button class="approve-all-btn" onclick={handleApproveAll}>
				<Icon name="check-circle" size={14} />
				Approve All ({pendingApprovalCount})
			</button>
		{/if}
	</div>

	{#if tasks.length > 0}
		<div class="task-stats">
			{#if pendingApprovalCount > 0}
				<span class="stat pending-approval">{pendingApprovalCount} pending approval</span>
			{/if}
			{#if approvedCount > 0}
				<span class="stat approved">{approvedCount} approved</span>
			{/if}
		</div>
	{/if}

	{#if tasks.length === 0}
		<div class="empty-tasks">
			<p>No tasks generated yet</p>
		</div>
	{:else}
		<div class="tasks">
			{#each tasks as task}
				{@const loop = getLoopForTask(task)}
				{@const isExpanded = expandedTasks.has(task.id)}
				{@const hasAC = task.acceptance_criteria && task.acceptance_criteria.length > 0}
				{@const statusInfo = getTaskStatusInfo(task.status)}
				<div class="task-item" data-status={task.status}>
					<div class="task-status" title={statusInfo.label}>
						<Icon name={getStatusIcon(task.status)} size={14} />
					</div>
					<div class="task-content">
						<div class="task-header">
							<span class="task-id">{task.sequence}</span>
							{#if task.type}
								<span class="task-type" title={getTaskTypeLabel(task.type)}>
									<Icon name={getTaskTypeIcon(task.type)} size={12} />
								</span>
							{/if}
							{#if task.iteration && task.max_iterations}
								<span class="task-iteration" title="Developer/Reviewer iteration">
									{task.iteration}/{task.max_iterations}
								</span>
							{/if}
							<span class="task-status-badge" data-status={task.status}>
								{statusInfo.label}
							</span>
						</div>
						<span class="task-description">{task.description}</span>

						{#if task.rejection_reason && task.status === 'rejected'}
							<div class="task-rejection-reason">
								<Icon name="x-circle" size={12} />
								<span>{task.rejection_reason}</span>
							</div>
						{/if}

						{#if task.rejection}
							{@const routing = getRejectionRouting(task.rejection.type)}
							<div class="task-rejection">
								<span class="rejection-type" data-action={routing.action}>
									{routing.label}
								</span>
								<span class="rejection-reason">{task.rejection.reason}</span>
							</div>
						{/if}

						{#if loop}
							<div class="task-agent">
								<AgentBadge
									role={loop.role}
									model={loop.model}
									state={loop.state}
									iterations={loop.iterations}
									maxIterations={loop.max_iterations}
								/>
							</div>
						{/if}

						{#if hasAC}
							<button
								class="ac-toggle"
								onclick={() => toggleExpand(task.id)}
								aria-expanded={isExpanded}
							>
								<Icon name={isExpanded ? 'chevron-down' : 'chevron-right'} size={12} />
								{task.acceptance_criteria.length} acceptance
								{task.acceptance_criteria.length === 1 ? 'criterion' : 'criteria'}
							</button>

							{#if isExpanded}
								<div class="acceptance-criteria">
									{#each task.acceptance_criteria as ac, i}
										<div class="ac-item">
											<div class="ac-line">
												<span class="ac-keyword">Given</span>
												<span class="ac-text">{ac.given}</span>
											</div>
											<div class="ac-line">
												<span class="ac-keyword">When</span>
												<span class="ac-text">{ac.when}</span>
											</div>
											<div class="ac-line">
												<span class="ac-keyword">Then</span>
												<span class="ac-text">{ac.then}</span>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						{/if}

						{#if task.files && task.files.length > 0}
							<div class="task-files">
								<Icon name="file" size={12} />
								<span>{task.files.join(', ')}</span>
							</div>
						{/if}

						{#if task.approved_by && task.approved_at}
							<div class="task-approval-info">
								<Icon name="check" size={12} />
								<span>Approved by {task.approved_by}</span>
							</div>
						{/if}

						<!-- Task Actions -->
						{#if canApproveTask(task) && (onApprove || onReject)}
							<div class="task-actions">
								{#if rejectingTaskId === task.id}
									<div class="reject-form">
										<input
											type="text"
											class="reject-reason-input"
											placeholder="Reason for rejection..."
											bind:value={rejectReason}
											onkeydown={(e) => e.key === 'Enter' && confirmReject()}
										/>
										<button
											class="btn btn-sm btn-danger"
											onclick={confirmReject}
											disabled={!rejectReason.trim()}
										>
											Reject
										</button>
										<button class="btn btn-sm btn-ghost" onclick={cancelReject}>
											Cancel
										</button>
									</div>
								{:else}
									<button class="btn btn-sm btn-success" onclick={() => handleApprove(task.id)}>
										<Icon name="check" size={12} />
										Approve
									</button>
									<button class="btn btn-sm btn-outline" onclick={() => startReject(task.id)}>
										<Icon name="x" size={12} />
										Reject
									</button>
									{#if onEdit && canEditTask(task)}
										<button class="btn btn-sm btn-ghost" onclick={() => onEdit(task)}>
											<Icon name="edit-2" size={12} />
										</button>
									{/if}
								{/if}
							</div>
						{:else if canEditTask(task) && (onEdit || onDelete)}
							<div class="task-actions">
								{#if onEdit}
									<button class="btn btn-sm btn-ghost" onclick={() => onEdit(task)}>
										<Icon name="edit-2" size={12} />
										Edit
									</button>
								{/if}
								{#if onDelete && canDeleteTask(task)}
									<button class="btn btn-sm btn-ghost btn-danger-text" onclick={() => onDelete(task.id)}>
										<Icon name="trash-2" size={12} />
									</button>
								{/if}
							</div>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.task-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.task-list-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.panel-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.task-count {
		font-variant-numeric: tabular-nums;
		color: var(--color-text-muted);
	}

	.approve-all-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-success);
		color: white;
		border: none;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.approve-all-btn:hover {
		background: var(--color-success-hover, #059669);
	}

	.task-stats {
		display: flex;
		gap: var(--space-3);
		font-size: var(--font-size-xs);
	}

	.stat {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-sm);
	}

	.stat.pending-approval {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.stat.approved {
		background: var(--color-success-muted, rgba(16, 185, 129, 0.1));
		color: var(--color-success);
	}

	.empty-tasks {
		padding: var(--space-4);
		text-align: center;
		color: var(--color-text-muted);
	}

	.tasks {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.task-item {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		border-left: 3px solid transparent;
	}

	.task-item[data-status='completed'] {
		border-left-color: var(--color-success);
	}

	.task-item[data-status='completed'] .task-status {
		color: var(--color-success);
	}

	.task-item[data-status='approved'] {
		border-left-color: var(--color-success);
		background: var(--color-success-muted, rgba(16, 185, 129, 0.05));
	}

	.task-item[data-status='approved'] .task-status {
		color: var(--color-success);
	}

	.task-item[data-status='pending_approval'] {
		border-left-color: var(--color-warning);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.05));
	}

	.task-item[data-status='pending_approval'] .task-status {
		color: var(--color-warning);
	}

	.task-item[data-status='rejected'] {
		border-left-color: var(--color-error);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.05));
	}

	.task-item[data-status='rejected'] .task-status {
		color: var(--color-error);
	}

	.task-item[data-status='in_progress'] {
		border-left-color: var(--color-accent);
		background: var(--color-accent-muted);
	}

	.task-item[data-status='in_progress'] .task-status {
		color: var(--color-accent);
	}

	.task-item[data-status='in_progress'] .task-status :global(svg) {
		animation: spin 1s linear infinite;
	}

	.task-item[data-status='failed'] {
		border-left-color: var(--color-error);
	}

	.task-item[data-status='failed'] .task-status {
		color: var(--color-error);
	}

	.task-item[data-status='pending'] .task-status {
		color: var(--color-text-muted);
	}

	.task-status {
		flex-shrink: 0;
		padding-top: 2px;
	}

	.task-content {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.task-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.task-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		min-width: 20px;
	}

	.task-type {
		color: var(--color-text-muted);
	}

	.task-iteration {
		font-size: var(--font-size-xs);
		color: var(--color-accent);
		font-family: var(--font-family-mono);
	}

	.task-status-badge {
		font-size: var(--font-size-xs);
		padding: 1px 6px;
		border-radius: var(--radius-sm);
		font-weight: var(--font-weight-medium);
	}

	.task-status-badge[data-status='pending'] {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.task-status-badge[data-status='pending_approval'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.2));
		color: var(--color-warning);
	}

	.task-status-badge[data-status='approved'] {
		background: var(--color-success-muted, rgba(16, 185, 129, 0.2));
		color: var(--color-success);
	}

	.task-status-badge[data-status='rejected'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.2));
		color: var(--color-error);
	}

	.task-status-badge[data-status='in_progress'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.task-status-badge[data-status='completed'] {
		background: var(--color-success-muted, rgba(16, 185, 129, 0.2));
		color: var(--color-success);
	}

	.task-status-badge[data-status='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.2));
		color: var(--color-error);
	}

	.task-description {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.task-rejection-reason {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-sm);
		margin-top: var(--space-1);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	.task-rejection-reason :global(svg) {
		flex-shrink: 0;
		margin-top: 2px;
	}

	.task-rejection {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		border-radius: var(--radius-sm);
		margin-top: var(--space-1);
	}

	.rejection-type {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.rejection-type[data-action='retry'] {
		color: var(--color-warning);
	}

	.rejection-type[data-action='plan'] {
		color: var(--color-error);
	}

	.rejection-type[data-action='decompose'] {
		color: var(--color-accent);
	}

	.rejection-reason {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.task-agent {
		margin-top: var(--space-1);
	}

	.task-approval-info {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-success);
		margin-top: var(--space-1);
	}

	.task-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		padding-top: var(--space-2);
		border-top: 1px solid var(--color-border);
	}

	.reject-form {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1;
	}

	.reject-reason-input {
		flex: 1;
		padding: var(--space-1) var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
	}

	.reject-reason-input:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-sm {
		padding: 4px 8px;
	}

	.btn-success {
		background: var(--color-success);
		color: white;
	}

	.btn-success:hover {
		background: var(--color-success-hover, #059669);
	}

	.btn-danger {
		background: var(--color-error);
		color: white;
	}

	.btn-danger:hover {
		background: var(--color-error-hover, #dc2626);
	}

	.btn-danger:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-outline {
		background: transparent;
		border-color: var(--color-border);
		color: var(--color-text-secondary);
	}

	.btn-outline:hover {
		border-color: var(--color-text-muted);
		color: var(--color-text-primary);
	}

	.btn-ghost {
		background: transparent;
		color: var(--color-text-muted);
	}

	.btn-ghost:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-danger-text {
		color: var(--color-error);
	}

	.btn-danger-text:hover {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
	}

	.ac-toggle {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) 0;
		background: transparent;
		border: none;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: color var(--transition-fast);
	}

	.ac-toggle:hover {
		color: var(--color-text-primary);
	}

	.acceptance-criteria {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		margin-top: var(--space-1);
	}

	.ac-item {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.ac-item + .ac-item {
		padding-top: var(--space-2);
		border-top: 1px solid var(--color-border);
	}

	.ac-line {
		display: flex;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
	}

	.ac-keyword {
		flex-shrink: 0;
		width: 50px;
		font-weight: var(--font-weight-semibold);
		color: var(--color-accent);
		text-transform: uppercase;
		font-size: var(--font-size-xs);
	}

	.ac-text {
		color: var(--color-text-primary);
	}

	.task-files {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
