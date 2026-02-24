<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import ChatDrawerTrigger from '$lib/components/chat/ChatDrawerTrigger.svelte';
	import TaskEditModal from '$lib/components/task/TaskEditModal.svelte';
	import { DataTable } from '$lib/components/table';
	import type { Column } from '$lib/components/table/types';
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
		planSlug: string;
		activeLoops?: ActiveLoop[];
		showAcceptanceCriteria?: boolean;
		canAddTask?: boolean;
		onApprove?: (taskId: string) => void | Promise<void>;
		onReject?: (taskId: string, reason: string) => void | Promise<void>;
		onDelete?: (taskId: string) => void | Promise<void>;
		onApproveAll?: () => void | Promise<void>;
		onTasksChange?: () => Promise<void>;
	}

	let {
		tasks,
		planSlug,
		activeLoops = [],
		showAcceptanceCriteria = false,
		canAddTask = false,
		onApprove,
		onReject,
		onDelete,
		onApproveAll,
		onTasksChange
	}: Props = $props();

	const pendingApprovalCount = $derived(tasks.filter((t) => t.status === 'pending_approval').length);

	// Track which task is being rejected (to show reason input)
	let rejectingTaskId = $state<string | null>(null);
	let rejectReason = $state('');

	// Track task operations in progress
	let processingTaskId = $state<string | null>(null);

	// Task edit modal state
	let showEditModal = $state(false);
	let editingTask = $state<Task | undefined>(undefined);

	function handleEdit(task: Task) {
		editingTask = task;
		showEditModal = true;
	}

	function handleCreate() {
		editingTask = undefined;
		showEditModal = true;
	}

	function closeModal() {
		showEditModal = false;
		editingTask = undefined;
	}

	async function handleModalSave() {
		await onTasksChange?.();
	}

	const columns: Column<Task>[] = [
		{ key: 'sequence', label: '#', width: '50px', sortable: true, align: 'center' },
		{ key: 'description', label: 'Description', sortable: true },
		{ key: 'type', label: 'Type', width: '100px', sortable: true, hideOnMobile: true },
		{ key: 'status', label: 'Status', width: '130px', sortable: true }
	];

	const statusOptions = [
		{ value: 'pending', label: 'Pending' },
		{ value: 'pending_approval', label: 'Pending Approval' },
		{ value: 'approved', label: 'Approved' },
		{ value: 'rejected', label: 'Rejected' },
		{ value: 'in_progress', label: 'In Progress' },
		{ value: 'completed', label: 'Completed' },
		{ value: 'failed', label: 'Failed' }
	];

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

	async function handleApprove(taskId: string) {
		if (processingTaskId) return; // Prevent double-clicks
		processingTaskId = taskId;
		try {
			await onApprove?.(taskId);
		} finally {
			processingTaskId = null;
		}
	}

	function startReject(taskId: string) {
		rejectingTaskId = taskId;
		rejectReason = '';
	}

	function cancelReject() {
		rejectingTaskId = null;
		rejectReason = '';
	}

	async function confirmReject() {
		if (rejectingTaskId && rejectReason.trim()) {
			const taskId = rejectingTaskId;
			const reason = rejectReason.trim();
			processingTaskId = taskId;
			try {
				await onReject?.(taskId, reason);
			} finally {
				processingTaskId = null;
				rejectingTaskId = null;
				rejectReason = '';
			}
		}
	}

	function handleApproveAll() {
		onApproveAll?.();
	}
</script>

<div class="task-list-wrapper">
	<DataTable
		data={tasks}
		{columns}
		filterPlaceholder="Search tasks..."
		filterFields={['description']}
		getRowKey={(task) => task.id}
		ariaLabel="Tasks table"
		countLabel="tasks"
		pageSize={20}
		expandable={true}
		{statusOptions}
		statusField="status"
		emptyMessage="No tasks generated yet"
		testIdPrefix="task-list"
	>
		{#snippet headerInfo()}
			{#if canAddTask}
				<button class="add-task-btn" onclick={handleCreate}>
					<Icon name="plus" size={14} />
					Add Task
				</button>
			{/if}
			{#if pendingApprovalCount > 0 && onApproveAll}
				<button class="approve-all-btn" onclick={handleApproveAll}>
					<Icon name="check-circle" size={14} />
					Approve All ({pendingApprovalCount})
				</button>
			{/if}
		{/snippet}

		{#snippet cell(column, task)}
			{#if column.key === 'sequence'}
				<span class="task-sequence">{task.sequence}</span>
			{:else if column.key === 'description'}
				<div class="task-description-cell">
					<span class="task-description">{task.description}</span>
					{#if task.iteration && task.max_iterations}
						<span class="task-iteration" title="Developer/Reviewer iteration">
							{task.iteration}/{task.max_iterations}
						</span>
					{/if}
				</div>
			{:else if column.key === 'type'}
				{#if task.type}
					<span class="task-type" title={getTaskTypeLabel(task.type)}>
						<Icon name={getTaskTypeIcon(task.type)} size={14} />
						<span>{getTaskTypeLabel(task.type)}</span>
					</span>
				{/if}
			{:else if column.key === 'status'}
				{@const statusInfo = getTaskStatusInfo(task.status)}
				<span class="task-status-badge" data-status={task.status}>
					<Icon name={statusInfo.icon} size={12} />
					{statusInfo.label}
				</span>
			{/if}
		{/snippet}

		{#snippet expandedRow(task)}
			{@const loop = getLoopForTask(task)}
			{@const hasAC = task.acceptance_criteria && task.acceptance_criteria.length > 0}

			<div class="task-details">
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
					<div class="acceptance-criteria">
						<h4>Acceptance Criteria</h4>
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
			</div>
		{/snippet}

		{#snippet actions(task)}
			{#if canApproveTask(task) && (onApprove || onReject)}
				{#if rejectingTaskId === task.id}
					<div class="reject-form">
						<input
							type="text"
							class="reject-reason-input"
							placeholder="Reason..."
							bind:value={rejectReason}
							onkeydown={(e) => e.key === 'Enter' && confirmReject()}
							aria-label="Rejection reason (required)"
						/>
						<button
							class="btn btn-sm btn-danger"
							onclick={confirmReject}
							disabled={!rejectReason.trim() || processingTaskId === task.id}
							aria-busy={processingTaskId === task.id}
						>
							{processingTaskId === task.id ? '...' : 'OK'}
						</button>
						<button
							class="btn btn-sm btn-ghost"
							onclick={cancelReject}
							disabled={processingTaskId === task.id}
						>
							<Icon name="x" size={12} />
						</button>
					</div>
				{:else}
					<div class="action-buttons">
						<button
							class="btn btn-sm btn-success"
							onclick={() => handleApprove(task.id)}
							title="Approve task"
							disabled={processingTaskId === task.id}
							aria-busy={processingTaskId === task.id}
						>
							<Icon name="check" size={12} />
						</button>
						<button
							class="btn btn-sm btn-outline"
							onclick={() => startReject(task.id)}
							title="Reject task"
							disabled={processingTaskId === task.id}
						>
							<Icon name="x" size={12} />
						</button>
						{#if canEditTask(task)}
							<button class="btn btn-sm btn-ghost" onclick={() => handleEdit(task)} title="Edit task">
								<Icon name="edit-2" size={12} />
							</button>
						{/if}
						<ChatDrawerTrigger
							context={{ type: 'task', taskId: task.id, planSlug }}
							variant="icon"
							class="task-chat-trigger"
						/>
					</div>
				{/if}
			{:else if canEditTask(task) || onDelete}
				<div class="action-buttons">
					{#if canEditTask(task)}
						<button class="btn btn-sm btn-ghost" onclick={() => handleEdit(task)} title="Edit task">
							<Icon name="edit-2" size={12} />
						</button>
					{/if}
					<ChatDrawerTrigger
						context={{ type: 'task', taskId: task.id, planSlug }}
						variant="icon"
						class="task-chat-trigger"
					/>
					{#if onDelete && canDeleteTask(task)}
						<button class="btn btn-sm btn-ghost btn-danger-text" onclick={() => onDelete(task.id)} title="Delete task">
							<Icon name="trash-2" size={12} />
						</button>
					{/if}
				</div>
			{:else}
				<div class="action-buttons">
					<ChatDrawerTrigger
						context={{ type: 'task', taskId: task.id, planSlug }}
						variant="icon"
						class="task-chat-trigger"
					/>
				</div>
			{/if}
		{/snippet}
	</DataTable>

	<TaskEditModal
		open={showEditModal}
		task={editingTask}
		{planSlug}
		allTasks={tasks}
		onClose={closeModal}
		onSave={handleModalSave}
	/>
</div>

<style>
	.task-list-wrapper {
		height: 100%;
	}

	.add-task-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-accent);
		color: white;
		border: none;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.add-task-btn:hover {
		background: var(--color-accent-hover);
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

	/* Chat trigger in task row */
	.action-buttons :global(.task-chat-trigger) {
		width: 24px;
		height: 24px;
		border: none;
		background: transparent;
	}

	.action-buttons :global(.task-chat-trigger:hover) {
		background: var(--color-bg-tertiary);
	}

	.task-sequence {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.task-description-cell {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.task-description {
		color: var(--color-text-primary);
	}

	.task-iteration {
		font-size: var(--font-size-xs);
		color: var(--color-accent);
		font-family: var(--font-family-mono);
	}

	.task-type {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.task-status-badge {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px 8px;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
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

	/* Expanded row details */
	.task-details {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.task-rejection-reason {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-2);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-sm);
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
	}

	.acceptance-criteria {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-sm);
	}

	.acceptance-criteria h4 {
		margin: 0 0 var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
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

	/* Actions */
	.action-buttons {
		display: flex;
		gap: var(--space-1);
	}

	.reject-form {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.reject-reason-input {
		width: 120px;
		padding: var(--space-1);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
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
		justify-content: center;
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
		padding: 4px 6px;
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
</style>
