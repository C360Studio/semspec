<script lang="ts">
	import type { PipelineStageState } from '$lib/types/changes';

	interface Props {
		proposal: PipelineStageState;
		design: PipelineStageState;
		spec: PipelineStageState;
		tasks: PipelineStageState;
		compact?: boolean;
	}

	let { proposal, design, spec, tasks, compact = false }: Props = $props();

	const stages = $derived([
		{ key: 'proposal', label: 'prop', state: proposal },
		{ key: 'design', label: 'dsgn', state: design },
		{ key: 'spec', label: 'spec', state: spec },
		{ key: 'tasks', label: 'task', state: tasks }
	]);

	function getStateIcon(state: PipelineStageState): string {
		switch (state) {
			case 'complete':
				return '\u2713'; // checkmark
			case 'generating':
				return '\u25CF'; // filled circle
			case 'failed':
				return '\u2717'; // x mark
			default:
				return '\u25CB'; // empty circle
		}
	}
</script>

<div class="pipeline" class:compact>
	{#each stages as stage, i}
		{#if i > 0}
			<div class="connector" class:active={stages[i - 1].state === 'complete'} aria-hidden="true"></div>
		{/if}
		<div
			class="stage"
			data-state={stage.state}
			role="status"
			aria-label="{stage.key}: {stage.state}"
		>
			<span class="icon" aria-hidden="true">{getStateIcon(stage.state)}</span>
			{#if !compact}
				<span class="label">{stage.label}</span>
			{:else}
				<span class="visually-hidden">{stage.key}: {stage.state}</span>
			{/if}
		</div>
	{/each}
</div>

<style>
	.pipeline {
		display: flex;
		align-items: center;
		gap: var(--space-1);
	}

	.pipeline.compact {
		gap: 2px;
	}

	.stage {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		font-size: var(--font-size-xs);
		transition: all var(--transition-fast);
	}

	.compact .stage {
		padding: 2px 4px;
	}

	.stage[data-state='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.stage[data-state='generating'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.stage[data-state='generating'] .icon {
		animation: pulse 1.5s ease-in-out infinite;
	}

	.stage[data-state='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.stage[data-state='none'] {
		color: var(--color-text-muted);
	}

	.icon {
		font-weight: var(--font-weight-semibold);
	}

	.label {
		color: inherit;
	}

	.connector {
		width: 8px;
		height: 2px;
		background: var(--color-border);
		transition: background var(--transition-fast);
	}

	.connector.active {
		background: var(--color-success);
	}

	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.5;
		}
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}
</style>
