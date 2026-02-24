<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { Phase } from '$lib/types/phase';
	import type { Task } from '$lib/types/task';
	import type { ActiveLoop } from '$lib/types/plan';
	import { getPhaseStatusInfo, canApprovePhase, canEditPhase, canDeletePhase } from '$lib/types/phase';
	import { getTaskStatusInfo, canApproveTask } from '$lib/types/task';

	interface Props {
		phase: Phase;
		tasks: Task[];
		allPhases: Phase[];
		activeLoops?: ActiveLoop[];
		expanded?: boolean;
		onToggle?: () => void;
		onEdit?: (phase: Phase) => void;
		onDelete?: (phase: Phase) => void;
		onApprove?: (phase: Phase) => void;
		onTaskApprove?: (taskId: string) => void;
		onTaskReject?: (taskId: string, reason: string) => void;
		onTaskEdit?: (task: Task) => void;
		onAddTask?: (phaseId: string) => void;
	}

	let {
		phase,
		tasks,
		allPhases,
		activeLoops = [],
		expanded = false,
		onToggle,
		onEdit,
		onDelete,
		onApprove,
		onTaskApprove,
		onTaskReject,
		onTaskEdit,
		onAddTask
	}: Props = $props();

	// Task stats for this phase
	const taskStats = $derived.by(() => {
		const phaseTasks = tasks.filter((t) => t.phase_id === phase.id);
		return {
			total: phaseTasks.length,
			completed: phaseTasks.filter((t) => t.status === 'completed').length,
			in_progress: phaseTasks.filter((t) => t.status === 'in_progress').length,
			pending: phaseTasks.filter(
				(t) => ['pending', 'pending_approval', 'approved'].includes(t.status)
			).length
		};
	});

	// Tasks belonging to this phase
	const phaseTasks = $derived(tasks.filter((t) => t.phase_id === phase.id));

	// Get dependency names
	const dependencyNames = $derived.by(() => {
		if (!phase.depends_on?.length) return [];
		return phase.depends_on
			.map((depId) => {
				const depPhase = allPhases.find((p) => p.id === depId);
				return depPhase?.name || depId;
			});
	});

	const statusInfo = $derived(getPhaseStatusInfo(phase.status as import('$lib/types/phase').PhaseStatus));
	const isBlocked = $derived(phase.status === 'blocked');
	const canEdit = $derived(canEditPhase(phase));
	const canDelete = $derived(canDeletePhase(phase));
	const needsApproval = $derived(canApprovePhase(phase));

	function handleToggle() {
		onToggle?.();
	}

	function handleEdit(e: Event) {
		e.stopPropagation();
		onEdit?.(phase);
	}

	function handleDelete(e: Event) {
		e.stopPropagation();
		if (confirm(`Delete phase "${phase.name}"?`)) {
			onDelete?.(phase);
		}
	}

	function handleApprove(e: Event) {
		e.stopPropagation();
		onApprove?.(phase);
	}
</script>

<div class="phase-card" class:expanded class:blocked={isBlocked} data-status={phase.status}>
	<button class="phase-header" onclick={handleToggle} aria-expanded={expanded}>
		<div class="header-left">
			<Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={16} />
			<span class="phase-name">{phase.name}</span>
			<span class="phase-status-badge" data-status={phase.status}>
				<Icon name={statusInfo.icon} size={12} />
				{statusInfo.label}
			</span>
		</div>
		<div class="header-right">
			<span class="task-progress">
				<span class="completed">{taskStats.completed}</span>
				<span class="separator">/</span>
				<span class="total">{taskStats.total}</span>
			</span>
			{#if taskStats.in_progress > 0}
				<span class="in-progress-indicator" title="{taskStats.in_progress} in progress">
					<Icon name="loader" size={12} />
				</span>
			{/if}
		</div>
	</button>

	{#if expanded}
		<div class="phase-content">
			{#if phase.description}
				<p class="phase-description">{phase.description}</p>
			{/if}

			{#if dependencyNames.length > 0}
				<div class="phase-dependencies">
					<Icon name="git-branch" size={12} />
					<span>Depends on: {dependencyNames.join(', ')}</span>
				</div>
			{/if}

			{#if needsApproval}
				<div class="approval-bar">
					<button class="btn btn-success btn-sm" onclick={handleApprove}>
						<Icon name="check" size={12} />
						Approve Phase
					</button>
				</div>
			{/if}

			<div class="phase-actions">
				{#if canEdit && onEdit}
					<button class="btn btn-ghost btn-sm" onclick={handleEdit} title="Edit phase">
						<Icon name="edit-2" size={12} />
					</button>
				{/if}
				{#if canDelete && onDelete}
					<button class="btn btn-ghost btn-sm btn-danger-text" onclick={handleDelete} title="Delete phase">
						<Icon name="trash-2" size={12} />
					</button>
				{/if}
				{#if onAddTask}
					<button class="btn btn-accent btn-sm" onclick={() => onAddTask?.(phase.id)}>
						<Icon name="plus" size={12} />
						Add Task
					</button>
				{/if}
			</div>

			{#if phaseTasks.length > 0}
				<div class="phase-tasks">
					{#each phaseTasks as task (task.id)}
						{@const taskStatus = getTaskStatusInfo(task.status)}
						<div class="task-row" data-status={task.status}>
							<span class="task-sequence">#{task.sequence}</span>
							<span class="task-description">{task.description}</span>
							<span class="task-status-badge" data-status={task.status}>
								<Icon name={taskStatus.icon} size={10} />
								{taskStatus.label}
							</span>
							<div class="task-actions">
								{#if canApproveTask(task) && onTaskApprove}
									<button
										class="btn btn-success btn-xs"
										onclick={() => onTaskApprove?.(task.id)}
										title="Approve"
									>
										<Icon name="check" size={10} />
									</button>
								{/if}
								{#if onTaskEdit}
									<button
										class="btn btn-ghost btn-xs"
										onclick={() => onTaskEdit?.(task)}
										title="Edit"
									>
										<Icon name="edit-2" size={10} />
									</button>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{:else}
				<p class="no-tasks">No tasks in this phase yet.</p>
			{/if}
		</div>
	{/if}
</div>

<style>
	.phase-card {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		overflow: hidden;
		transition: all var(--transition-fast);
	}

	.phase-card.blocked {
		opacity: 0.7;
		border-style: dashed;
	}

	.phase-card.expanded {
		box-shadow: var(--shadow-sm);
	}

	.phase-card[data-status='complete'] {
		border-left: 3px solid var(--color-success);
	}

	.phase-card[data-status='active'] {
		border-left: 3px solid var(--color-accent);
	}

	.phase-card[data-status='failed'] {
		border-left: 3px solid var(--color-error);
	}

	.phase-header {
		width: 100%;
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-3) var(--space-4);
		background: transparent;
		border: none;
		cursor: pointer;
		text-align: left;
		transition: background var(--transition-fast);
	}

	.phase-header:hover {
		background: var(--color-bg-secondary);
	}

	.header-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.header-right {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.phase-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.phase-status-badge {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
	}

	.phase-status-badge[data-status='pending'] {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.phase-status-badge[data-status='ready'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.2));
		color: var(--color-warning);
	}

	.phase-status-badge[data-status='active'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.phase-status-badge[data-status='complete'] {
		background: var(--color-success-muted, rgba(16, 185, 129, 0.2));
		color: var(--color-success);
	}

	.phase-status-badge[data-status='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.2));
		color: var(--color-error);
	}

	.phase-status-badge[data-status='blocked'] {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.15));
		color: var(--color-warning);
	}

	.task-progress {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.task-progress .completed {
		color: var(--color-success);
		font-weight: var(--font-weight-medium);
	}

	.task-progress .separator {
		color: var(--color-border);
	}

	.in-progress-indicator {
		display: flex;
		align-items: center;
		color: var(--color-accent);
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		to { transform: rotate(360deg); }
	}

	.phase-content {
		padding: 0 var(--space-4) var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.phase-description {
		margin: var(--space-3) 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.phase-dependencies {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.approval-bar {
		display: flex;
		justify-content: flex-start;
		margin: var(--space-3) 0;
		padding: var(--space-2);
		background: var(--color-success-muted, rgba(16, 185, 129, 0.1));
		border-radius: var(--radius-sm);
	}

	.phase-actions {
		display: flex;
		gap: var(--space-2);
		margin: var(--space-3) 0;
	}

	.phase-tasks {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		margin-top: var(--space-3);
	}

	.task-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-sm);
		transition: background var(--transition-fast);
	}

	.task-row:hover {
		background: var(--color-bg-tertiary);
	}

	.task-row[data-status='completed'] {
		opacity: 0.7;
	}

	.task-sequence {
		flex-shrink: 0;
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		width: 32px;
	}

	.task-description {
		flex: 1;
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.task-status-badge {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
	}

	.task-status-badge[data-status='pending'],
	.task-status-badge[data-status='pending_approval'] {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.task-status-badge[data-status='approved'] {
		background: var(--color-success-muted, rgba(16, 185, 129, 0.2));
		color: var(--color-success);
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

	.task-actions {
		display: flex;
		gap: 4px;
	}

	.no-tasks {
		margin: var(--space-3) 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		font-style: italic;
	}

	/* Buttons */
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border: none;
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-sm {
		padding: 4px 8px;
	}

	.btn-xs {
		padding: 2px 4px;
		font-size: 10px;
	}

	.btn-success {
		background: var(--color-success);
		color: white;
	}

	.btn-success:hover {
		background: var(--color-success-hover, #059669);
	}

	.btn-accent {
		background: var(--color-accent);
		color: white;
	}

	.btn-accent:hover {
		background: var(--color-accent-hover);
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
