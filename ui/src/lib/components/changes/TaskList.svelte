<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import type { Task } from '$lib/types/task';
	import type { ActiveLoop } from '$lib/types/plan';
	import { getTaskTypeLabel, getRejectionRouting } from '$lib/types/task';

	interface Props {
		tasks: Task[];
		activeLoops?: ActiveLoop[];
		showAcceptanceCriteria?: boolean;
	}

	let { tasks, activeLoops = [], showAcceptanceCriteria = false }: Props = $props();

	const completedCount = $derived(tasks.filter((t) => t.status === 'completed').length);

	// Track which tasks have expanded acceptance criteria
	let expandedTasks = $state<Set<string>>(new Set());

	function toggleExpand(taskId: string) {
		if (expandedTasks.has(taskId)) {
			expandedTasks.delete(taskId);
		} else {
			expandedTasks.add(taskId);
		}
		expandedTasks = new Set(expandedTasks);
	}

	function getStatusIcon(status: Task['status']): string {
		switch (status) {
			case 'completed':
				return 'check';
			case 'in_progress':
				return 'loader';
			case 'failed':
				return 'x';
			default:
				return 'circle';
		}
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
</script>

<div class="task-list">
	<h3 class="panel-title">
		Tasks
		<span class="task-count">{completedCount}/{tasks.length}</span>
	</h3>

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
				<div class="task-item" data-status={task.status}>
					<div class="task-status">
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
						</div>
						<span class="task-description">{task.description}</span>

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

	.panel-title {
		display: flex;
		justify-content: space-between;
		align-items: center;
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

	.task-description {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
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
