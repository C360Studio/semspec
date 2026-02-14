<script lang="ts">
	import type { PlanPhaseState } from '$lib/types/plan';

	interface Props {
		plan: PlanPhaseState;
		tasks: PlanPhaseState;
		execute: PlanPhaseState;
		compact?: boolean;
	}

	let { plan, tasks, execute, compact = false }: Props = $props();

	const phases = $derived([
		{ key: 'plan', label: 'plan', state: plan },
		{ key: 'tasks', label: 'tasks', state: tasks },
		{ key: 'execute', label: 'exec', state: execute }
	]);

	function getStateIcon(state: PlanPhaseState): string {
		switch (state) {
			case 'complete':
				return '\u2713'; // checkmark
			case 'active':
				return '\u25CF'; // filled circle
			case 'failed':
				return '\u2717'; // x mark
			default:
				return '\u25CB'; // empty circle
		}
	}
</script>

<div class="pipeline" class:compact>
	{#each phases as phase, i}
		{#if i > 0}
			<div class="connector" class:active={phases[i - 1].state === 'complete'} aria-hidden="true"></div>
		{/if}
		<div
			class="phase"
			data-state={phase.state}
			role="status"
			aria-label="{phase.key}: {phase.state}"
		>
			<span class="icon" aria-hidden="true">{getStateIcon(phase.state)}</span>
			{#if !compact}
				<span class="label">{phase.label}</span>
			{:else}
				<span class="visually-hidden">{phase.key}: {phase.state}</span>
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

	.phase {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		font-size: var(--font-size-xs);
		transition: all var(--transition-fast);
	}

	.compact .phase {
		padding: 2px 4px;
	}

	.phase[data-state='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.phase[data-state='active'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.phase[data-state='active'] .icon {
		animation: pulse 1.5s ease-in-out infinite;
	}

	.phase[data-state='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.phase[data-state='none'] {
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
