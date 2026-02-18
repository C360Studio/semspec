<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';

	interface Props {
		role: string;
		model?: string;
		state: string;
		iterations?: number;
		maxIterations?: number;
	}

	let { role, model, state, iterations, maxIterations }: Props = $props();

	const progress = $derived(
		iterations !== undefined && maxIterations !== undefined && maxIterations > 0
			? (iterations / maxIterations) * 100
			: 0
	);

	const isActive = $derived(state === 'executing');
</script>

<div class="agent-badge" class:active={isActive}>
	<Icon name="bot" size={14} />
	<span class="role">{role}</span>
	<span class="model">({model})</span>
	{#if iterations !== undefined && maxIterations !== undefined}
		<span class="progress-text">{iterations}/{maxIterations}</span>
		{#if isActive}
			<div class="progress-bar" title="{progress.toFixed(0)}% complete">
				<div class="progress-fill" style="width: {progress}%"></div>
			</div>
		{/if}
	{/if}
</div>

<style>
	.agent-badge {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.agent-badge.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.role {
		font-weight: var(--font-weight-medium);
	}

	.model {
		opacity: 0.8;
	}

	.progress-text {
		margin-left: var(--space-1);
		font-variant-numeric: tabular-nums;
	}

	.progress-bar {
		width: 32px;
		height: 4px;
		background: var(--color-bg-primary);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--color-accent);
		transition: width var(--transition-fast);
	}
</style>
