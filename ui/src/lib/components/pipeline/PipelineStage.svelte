<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { PipelineStageState, StageState } from '$lib/types/pipeline';
	import { getStateIcon, getStateClass } from '$lib/types/pipeline';

	interface Props {
		/** Stage state */
		stageState: PipelineStageState;
		/** Compact mode */
		compact?: boolean;
		/** Show iteration progress */
		showProgress?: boolean;
	}

	let { stageState, compact = false, showProgress = true }: Props = $props();

	const { stage, state, iterations, maxIterations } = $derived(stageState);
	const stateIcon = $derived(getStateIcon(state));
	const stateClass = $derived(getStateClass(state));
	const isActive = $derived(state === 'active');
	const progress = $derived(
		iterations !== undefined && maxIterations !== undefined && maxIterations > 0
			? Math.round((iterations / maxIterations) * 100)
			: 0
	);
</script>

<div
	class="pipeline-stage"
	class:compact
	class:active={isActive}
	class:complete={state === 'complete'}
	class:failed={state === 'failed'}
	class:pending={state === 'pending'}
	class:parallel={stage.parallel}
>
	<div class="stage-icon {stateClass}">
		<Icon name={isActive ? 'loader' : stateIcon} size={compact ? 12 : 14} class={isActive ? 'spin' : ''} />
	</div>

	<div class="stage-content">
		<span class="stage-label">{compact ? stage.shortLabel : stage.label}</span>

		{#if showProgress && isActive && iterations !== undefined}
			<span class="stage-progress">
				{iterations}/{maxIterations}
			</span>
		{/if}
	</div>

	{#if isActive && showProgress}
		<div class="progress-bar">
			<div class="progress-fill" style="width: {progress}%"></div>
		</div>
	{/if}
</div>

<style>
	.pipeline-stage {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		min-width: 80px;
		position: relative;
	}

	.pipeline-stage.compact {
		padding: var(--space-1) var(--space-2);
		min-width: 60px;
	}

	.pipeline-stage.active {
		border-color: var(--color-info);
		background: var(--color-info-muted);
	}

	.pipeline-stage.complete {
		border-color: var(--color-success);
	}

	.pipeline-stage.failed {
		border-color: var(--color-error);
	}

	.pipeline-stage.parallel {
		min-width: 70px;
	}

	.stage-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		border-radius: var(--radius-full);
	}

	.compact .stage-icon {
		width: 20px;
		height: 20px;
	}

	.stage-icon.neutral {
		background: var(--color-bg-elevated);
		color: var(--color-text-muted);
	}

	.stage-icon.info {
		background: var(--color-info);
		color: white;
	}

	.stage-icon.success {
		background: var(--color-success);
		color: white;
	}

	.stage-icon.error {
		background: var(--color-error);
		color: white;
	}

	.stage-content {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
	}

	.stage-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		text-align: center;
		white-space: nowrap;
	}

	.compact .stage-label {
		font-size: 10px;
	}

	.stage-progress {
		font-size: 10px;
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.progress-bar {
		position: absolute;
		bottom: 0;
		left: 0;
		right: 0;
		height: 3px;
		background: var(--color-bg-elevated);
		border-radius: 0 0 var(--radius-md) var(--radius-md);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--color-info);
		transition: width var(--transition-base);
	}

	:global(.spin) {
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
