<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import type { ParsedTask, ActiveLoop } from '$lib/types/changes';

	interface Props {
		tasks: ParsedTask[];
		activeLoops?: ActiveLoop[];
	}

	let { tasks, activeLoops = [] }: Props = $props();

	const completedCount = $derived(tasks.filter((t) => t.status === 'complete').length);

	function getStatusIcon(status: ParsedTask['status']): string {
		switch (status) {
			case 'complete':
				return 'check';
			case 'in_progress':
				return 'loader';
			case 'failed':
				return 'x';
			case 'blocked':
				return 'pause';
			default:
				return 'circle';
		}
	}

	function getLoopForTask(task: ParsedTask): ActiveLoop | undefined {
		if (!task.assigned_loop_id) return undefined;
		return activeLoops.find((l) => l.loop_id === task.assigned_loop_id);
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
				<div class="task-item" data-status={task.status}>
					<div class="task-status">
						<Icon name={getStatusIcon(task.status)} size={14} />
					</div>
					<div class="task-content">
						<span class="task-id">{task.id}</span>
						<span class="task-description">{task.description}</span>
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
						{#if task.status === 'blocked' && task.blocked_by}
							<span class="task-blocked">
								<Icon name="clock" size={12} />
								blocked by {task.blocked_by.join(', ')}
							</span>
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

	.task-item[data-status='complete'] {
		border-left-color: var(--color-success);
	}

	.task-item[data-status='complete'] .task-status {
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

	.task-item[data-status='blocked'] {
		border-left-color: var(--color-warning);
		opacity: 0.7;
	}

	.task-item[data-status='blocked'] .task-status {
		color: var(--color-warning);
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

	.task-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.task-description {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.task-agent {
		margin-top: var(--space-1);
	}

	.task-blocked {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-warning);
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
