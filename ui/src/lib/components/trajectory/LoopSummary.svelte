<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { Trajectory } from '$lib/types/trajectory';

	interface Props {
		role: string;
		state: string;
		trajectory?: Trajectory;
	}

	let { role, state, trajectory }: Props = $props();

	const totalTokens = $derived(
		trajectory ? trajectory.total_tokens_in + trajectory.total_tokens_out : 0
	);

	const isActive = $derived(state === 'executing' || state === 'pending');
	const isComplete = $derived(state === 'completed');
	const isFailed = $derived(state === 'failed' || state === 'error');

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(1)}s`;
	}

	function roleIcon(r: string): string {
		switch (r) {
			case 'planner':
			case 'planning':
				return 'edit-3';
			case 'reviewer':
			case 'reviewing':
			case 'plan-reviewer':
				return 'check-square';
			case 'tester':
			case 'testing':
				return 'flask-conical';
			case 'builder':
			case 'building':
				return 'hammer';
			case 'validator':
			case 'validation':
				return 'shield-check';
			case 'decomposer':
				return 'git-branch';
			default:
				return 'cpu';
		}
	}
</script>

<div class="loop-summary" class:active={isActive} class:complete={isComplete} class:failed={isFailed}>
	<div class="loop-left">
		<Icon name={roleIcon(role)} size={12} />
		<span class="loop-role">{role}</span>
		<span class="loop-state" class:state-active={isActive} class:state-complete={isComplete} class:state-failed={isFailed}>
			{state}
		</span>
	</div>
	<div class="loop-right">
		{#if trajectory}
			{@const modelCalls = trajectory.steps.filter((s) => s.step_type === 'model_call').length}
			{@const toolCalls = trajectory.steps.filter((s) => s.step_type === 'tool_call').length}
			{#if modelCalls > 0}
				<span class="loop-stat">{modelCalls} LLM</span>
			{/if}
			{#if toolCalls > 0}
				<span class="loop-stat">{toolCalls} tools</span>
			{/if}
			{#if totalTokens > 0}
				<span class="loop-stat">{formatTokens(totalTokens)} tok</span>
			{/if}
			{#if trajectory.duration > 0}
				<span class="loop-stat">{formatDuration(trajectory.duration)}</span>
			{/if}
		{:else if isActive}
			<span class="loop-stat loading">running...</span>
		{/if}
	</div>
</div>

<style>
	.loop-summary {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
		border-radius: var(--radius-sm);
		background: var(--color-bg-secondary);
	}

	.loop-summary.active {
		background: color-mix(in srgb, var(--color-accent-muted) 30%, var(--color-bg-secondary));
	}

	.loop-summary.failed {
		background: color-mix(in srgb, var(--color-error-muted) 30%, var(--color-bg-secondary));
	}

	.loop-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-secondary);
	}

	.loop-role {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.loop-state {
		padding: 1px var(--space-1);
		border-radius: var(--radius-sm);
		font-size: 10px;
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.state-active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.state-complete {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.state-failed {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.loop-right {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.loop-stat {
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.loop-stat.loading {
		font-family: var(--font-family-base);
		font-style: italic;
	}
</style>
