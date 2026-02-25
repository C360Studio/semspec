<script lang="ts">
	/**
	 * PlanNavTree - Progressive disclosure navigation tree for plans.
	 *
	 * Shows plan → phases → tasks hierarchy with:
	 * - Expand/collapse for phases (only shown after plan approved)
	 * - Task nodes within expanded phases (only shown after phase approved)
	 * - Status indicators and selection highlighting
	 * - Keyboard navigation support
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Phase, PhaseStatus } from '$lib/types/phase';
	import type { Task, TaskStatus } from '$lib/types/task';
	import type { PlanSelection, SelectionType } from '$lib/stores/planSelection.svelte';

	interface Props {
		plan: PlanWithStatus;
		phases: Phase[];
		tasksByPhase: Record<string, Task[]>;
		selection: PlanSelection | null;
		onSelect: (selection: PlanSelection) => void;
	}

	let { plan, phases, tasksByPhase, selection, onSelect }: Props = $props();

	// Track which phases are manually expanded (beyond auto-expand from selection)
	let manuallyExpanded = $state<Set<string>>(new Set());

	// A phase is expanded if it's manually expanded or contains the selected task
	function isExpanded(phaseId: string): boolean {
		if (manuallyExpanded.has(phaseId)) return true;
		if (selection?.phaseId === phaseId) return true;
		return false;
	}

	function togglePhase(phaseId: string): void {
		const newExpanded = new Set(manuallyExpanded);
		if (newExpanded.has(phaseId)) {
			newExpanded.delete(phaseId);
		} else {
			newExpanded.add(phaseId);
		}
		manuallyExpanded = newExpanded;
	}

	function handleSelectPlan(): void {
		onSelect({
			type: 'plan',
			planSlug: plan.slug
		});
	}

	function handleSelectPhase(phaseId: string): void {
		onSelect({
			type: 'phase',
			planSlug: plan.slug,
			phaseId
		});
	}

	function handleSelectTask(phaseId: string, taskId: string): void {
		onSelect({
			type: 'task',
			planSlug: plan.slug,
			phaseId,
			taskId
		});
	}

	// Check if a specific item is selected
	function isSelected(type: SelectionType, id?: string): boolean {
		if (!selection) return false;
		if (type === 'plan') {
			return selection.type === 'plan';
		}
		if (type === 'phase') {
			return selection.type === 'phase' && selection.phaseId === id;
		}
		if (type === 'task') {
			return selection.type === 'task' && selection.taskId === id;
		}
		return false;
	}

	// Get status icon for phases
	function getPhaseStatusIcon(status: PhaseStatus): string {
		switch (status) {
			case 'complete':
				return 'check-circle';
			case 'active':
				return 'loader';
			case 'failed':
				return 'x-circle';
			case 'blocked':
				return 'lock';
			case 'ready':
				return 'play-circle';
			default:
				return 'circle';
		}
	}

	// Get status icon for tasks
	function getTaskStatusIcon(status: TaskStatus): string {
		switch (status) {
			case 'completed':
				return 'check';
			case 'in_progress':
				return 'loader';
			case 'failed':
				return 'x';
			case 'approved':
				return 'check-circle';
			case 'rejected':
				return 'x-circle';
			case 'pending_approval':
				return 'clock';
			default:
				return 'circle';
		}
	}

	// Get task count for a phase
	function getTaskCount(phaseId: string): number {
		return tasksByPhase[phaseId]?.length ?? 0;
	}

	// Get completed task count for a phase
	function getCompletedCount(phaseId: string): number {
		return tasksByPhase[phaseId]?.filter((t) => t.status === 'completed').length ?? 0;
	}

	// Determine if phases should be shown (plan is approved)
	const showPhases = $derived(plan.approved && phases.length > 0);

	// Keyboard navigation
	function handleKeyDown(e: KeyboardEvent, action: () => void): void {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			action();
		}
	}
</script>

<div class="plan-nav-tree" role="tree" aria-label="Plan structure">
	<!-- Plan root node -->
	<button
		class="tree-node plan-node"
		class:selected={isSelected('plan')}
		onclick={handleSelectPlan}
		onkeydown={(e) => handleKeyDown(e, handleSelectPlan)}
		role="treeitem"
		aria-selected={isSelected('plan')}
		aria-level={1}
	>
		<span class="node-icon plan-icon">
			<Icon name="file-text" size={16} />
		</span>
		<span class="node-label">{plan.title || plan.slug}</span>
		{#if !plan.approved}
			<span class="node-badge draft">Draft</span>
		{/if}
	</button>

	<!-- Phase nodes -->
	{#if showPhases}
		<div class="tree-children" role="group">
			{#each phases as phase (phase.id)}
				{@const expanded = isExpanded(phase.id)}
				{@const phaseTasks = tasksByPhase[phase.id] ?? []}
				{@const hasApprovedPhase = phase.approved}
				{@const hasTasks = phaseTasks.length > 0}

				<div class="tree-branch">
					<div class="phase-row">
						<!-- Expand/collapse button (only if phase has tasks) -->
						{#if hasTasks}
							<button
								class="expand-btn"
								onclick={() => togglePhase(phase.id)}
								aria-expanded={expanded}
								aria-label={expanded ? 'Collapse phase' : 'Expand phase'}
							>
								<Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={14} />
							</button>
						{:else}
							<span class="expand-placeholder"></span>
						{/if}

						<!-- Phase node -->
						<button
							class="tree-node phase-node"
							class:selected={isSelected('phase', phase.id)}
							onclick={() => handleSelectPhase(phase.id)}
							onkeydown={(e) => handleKeyDown(e, () => handleSelectPhase(phase.id))}
							role="treeitem"
							aria-selected={isSelected('phase', phase.id)}
							aria-level={2}
							aria-expanded={hasTasks ? expanded : undefined}
						>
							<span class="node-icon phase-status" data-status={phase.status}>
								<Icon name={getPhaseStatusIcon(phase.status as PhaseStatus)} size={14} />
							</span>
							<span class="node-label">{phase.name}</span>
							{#if hasTasks}
								<span class="task-count">
									{getCompletedCount(phase.id)}/{getTaskCount(phase.id)}
								</span>
							{:else if hasApprovedPhase}
								<span class="node-badge pending">No tasks</span>
							{:else}
								<span class="node-badge awaiting">Awaiting approval</span>
							{/if}
						</button>
					</div>

					<!-- Task nodes (if expanded and has tasks) -->
					{#if expanded && hasTasks}
						<div class="tree-children tasks" role="group">
							{#each phaseTasks as task (task.id)}
								<button
									class="tree-node task-node"
									class:selected={isSelected('task', task.id)}
									onclick={() => handleSelectTask(phase.id, task.id)}
									onkeydown={(e) => handleKeyDown(e, () => handleSelectTask(phase.id, task.id))}
									role="treeitem"
									aria-selected={isSelected('task', task.id)}
									aria-level={3}
								>
									<span class="node-icon task-status" data-status={task.status}>
										<Icon name={getTaskStatusIcon(task.status)} size={12} />
									</span>
									<span class="node-label">{task.description}</span>
								</button>
							{/each}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{:else if plan.approved && phases.length === 0}
		<!-- Plan approved but no phases yet -->
		<div class="tree-empty">
			<Icon name="layers" size={14} />
			<span>Generating phases...</span>
		</div>
	{/if}
</div>

<style>
	.plan-nav-tree {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2);
	}

	.tree-node {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-2) var(--space-3);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-md);
		text-align: left;
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.tree-node:hover {
		background: var(--color-bg-tertiary);
	}

	.tree-node:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}

	.tree-node.selected {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
	}

	/* Plan node styling */
	.plan-node {
		font-weight: var(--font-weight-semibold);
	}

	.plan-icon {
		color: var(--color-accent);
	}

	/* Phase row with expand button */
	.phase-row {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.expand-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		padding: 0;
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
		flex-shrink: 0;
	}

	.expand-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.expand-placeholder {
		width: 20px;
		flex-shrink: 0;
	}

	/* Phase node */
	.phase-node {
		flex: 1;
	}

	/* Tree structure */
	.tree-children {
		margin-left: var(--space-4);
		padding-left: var(--space-2);
		border-left: 1px solid var(--color-border);
	}

	.tree-children.tasks {
		margin-left: calc(20px + var(--space-1));
	}

	.tree-branch {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	/* Task node */
	.task-node {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.task-node .node-label {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	/* Node icon styling */
	.node-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}

	.node-label {
		flex: 1;
		min-width: 0;
	}

	/* Status colors */
	.phase-status[data-status='complete'],
	.task-status[data-status='completed'],
	.task-status[data-status='approved'] {
		color: var(--color-success);
	}

	.phase-status[data-status='active'],
	.task-status[data-status='in_progress'] {
		color: var(--color-accent);
	}

	.phase-status[data-status='failed'],
	.task-status[data-status='failed'],
	.task-status[data-status='rejected'] {
		color: var(--color-error);
	}

	.phase-status[data-status='blocked'] {
		color: var(--color-warning);
	}

	.phase-status[data-status='ready'],
	.task-status[data-status='pending_approval'] {
		color: var(--color-warning);
	}

	.phase-status[data-status='pending'],
	.task-status[data-status='pending'] {
		color: var(--color-text-muted);
	}

	/* Badges and counts */
	.node-badge {
		font-size: var(--font-size-xs);
		padding: 1px var(--space-1);
		border-radius: var(--radius-sm);
		flex-shrink: 0;
	}

	.node-badge.draft {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.node-badge.pending {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.node-badge.awaiting {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		font-size: 10px;
	}

	.task-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	/* Empty state */
	.tree-empty {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		margin-left: var(--space-4);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	/* Loader animation for active/in_progress */
	.phase-status[data-status='active'] :global(svg),
	.task-status[data-status='in_progress'] :global(svg) {
		animation: spin 1s linear infinite;
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
