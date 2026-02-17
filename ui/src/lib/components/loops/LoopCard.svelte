<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import ContextPanel from '../context/ContextPanel.svelte';
	import TrajectoryPanel from '../trajectory/TrajectoryPanel.svelte';
	import type { Loop, ActivityEvent, LoopState } from '$lib/types';

	interface Props {
		loop: Loop;
		latestActivity?: ActivityEvent;
		onPause?: () => void;
		onResume?: () => void;
		onCancel?: () => void;
		/** Whether to show context toggle */
		showContext?: boolean;
		/** Plan slug to link to when workflowSlug is not available */
		planSlug?: string;
	}

	let { loop, latestActivity, onPause, onResume, onCancel, showContext = true, planSlug }: Props = $props();

	let contextExpanded = $state(false);
	let trajectoryExpanded = $state(false);

	// Extract workflow context from loop
	const workflowSlug = $derived(loop.workflow_slug);
	const workflowStep = $derived(loop.workflow_step);
	// Role and model not yet in generated types - keep as any for now
	const role = $derived((loop as any).role);
	const model = $derived((loop as any).model);
	const contextRequestId = $derived(loop.context_request_id);

	// Derive display values
	const shortId = $derived(loop.loop_id.slice(0, 8));
	const loopState = $derived(loop.state as LoopState);
	const isActive = $derived(['pending', 'exploring', 'executing'].includes(loopState));
	const isPaused = $derived(loopState === 'paused');
	const isComplete = $derived(['complete', 'success', 'failed', 'cancelled'].includes(loopState));

	// Format step name for display
	function formatStep(step?: string): string {
		if (!step) return '';
		return step.charAt(0).toUpperCase() + step.slice(1);
	}

	// Format role to step (fallback if workflow_step not available)
	function roleToStep(role?: string): string {
		if (!role) return '';
		const match = role.match(/^(\w+)-writer$/);
		return match ? formatStep(match[1]) : role;
	}

	// Get activity type icon
	function getActivityIcon(type?: string): string {
		switch (type) {
			case 'tool_call': return 'wrench';
			case 'tool_result': return 'check-circle';
			case 'model_request': return 'brain';
			case 'model_response': return 'message-square';
			default: return 'activity';
		}
	}
</script>

<div class="loop-card" class:active={isActive} class:paused={isPaused} class:complete={isComplete}>
	<div class="loop-header">
		<span class="loop-id" title={loop.loop_id}>{shortId}</span>
		<span class="state-badge" class:executing={isActive} class:paused={isPaused} class:complete={isComplete}>
			{loopState}
		</span>
	</div>

	{#if workflowSlug || role || planSlug}
		<div class="workflow-context">
			{#if workflowSlug}
				<span class="workflow-slug">{workflowSlug}</span>
				<span class="separator">&rarr;</span>
			{:else if planSlug}
				<a href="/plans/{planSlug}" class="plan-link">{planSlug}</a>
				<span class="separator">&rarr;</span>
			{/if}
			<span class="workflow-step">{formatStep(workflowStep) || roleToStep(role) || 'Working'}</span>
		</div>
	{/if}

	<div class="loop-progress">
		<div class="progress-bar">
			<div
				class="progress-fill"
				style="width: {(loop.iterations / loop.max_iterations) * 100}%"
			></div>
		</div>
		<span class="progress-text">{loop.iterations}/{loop.max_iterations}</span>
	</div>

	{#if latestActivity}
		<div class="latest-activity">
			<Icon name={getActivityIcon(latestActivity.type)} size={12} />
			<span class="activity-type">{latestActivity.type}</span>
		</div>
	{/if}

	{#if model}
		<div class="model-info">
			<Icon name="cpu" size={12} />
			<span>{model}</span>
		</div>
	{/if}

	<div class="loop-actions">
		{#if isActive && onPause}
			<button class="action-btn pause" onclick={onPause} title="Pause loop">
				<Icon name="pause" size={14} />
			</button>
		{/if}
		{#if isPaused && onResume}
			<button class="action-btn resume" onclick={onResume} title="Resume loop">
				<Icon name="play" size={14} />
			</button>
		{/if}
		{#if !isComplete && onCancel}
			<button class="action-btn cancel" onclick={onCancel} title="Cancel loop">
				<Icon name="x" size={14} />
			</button>
		{/if}
		{#if showContext && contextRequestId}
			<button
				class="action-btn context"
				class:active={contextExpanded}
				onclick={() => (contextExpanded = !contextExpanded)}
				title={contextExpanded ? 'Hide context' : 'Show context'}
			>
				<Icon name="layers" size={14} />
			</button>
		{/if}
		<button
			class="action-btn trajectory"
			class:active={trajectoryExpanded}
			onclick={() => (trajectoryExpanded = !trajectoryExpanded)}
			title={trajectoryExpanded ? 'Hide trajectory' : 'Show trajectory'}
		>
			<Icon name="git-branch" size={14} />
		</button>
	</div>

	{#if showContext && contextExpanded && contextRequestId}
		<div class="context-section">
			<ContextPanel requestId={contextRequestId} compact={true} />
		</div>
	{/if}

	{#if trajectoryExpanded}
		<div class="trajectory-section">
			<TrajectoryPanel loopId={loop.loop_id} compact={true} />
		</div>
	{/if}
</div>

<style>
	.loop-card {
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.loop-card.active {
		border-color: var(--color-accent);
	}

	.loop-card.paused {
		border-color: var(--color-warning);
	}

	.loop-card.complete {
		opacity: 0.7;
	}

	.loop-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.loop-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.state-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-full);
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.state-badge.executing {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.state-badge.paused {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.state-badge.complete {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.workflow-context {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-sm);
	}

	.workflow-slug {
		color: var(--color-text-primary);
		font-weight: var(--font-weight-medium);
	}

	.separator {
		color: var(--color-text-muted);
	}

	.workflow-step {
		color: var(--color-accent);
	}

	.plan-link {
		font-size: var(--font-size-xs);
		padding: 1px 4px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-sm);
		text-decoration: none;
	}

	.plan-link:hover {
		background: var(--color-accent);
		color: white;
		text-decoration: none;
	}

	.loop-progress {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.progress-bar {
		flex: 1;
		height: 4px;
		background: var(--color-bg-elevated);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--color-accent);
		transition: width var(--transition-base);
	}

	.progress-text {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	.latest-activity,
	.model-info {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.activity-type {
		font-family: var(--font-family-mono);
	}

	.loop-actions {
		display: flex;
		gap: var(--space-1);
		margin-top: var(--space-1);
	}

	.action-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.action-btn.pause {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.action-btn.resume {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.action-btn.cancel {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.action-btn:hover {
		filter: brightness(1.2);
	}

	.action-btn.context,
	.action-btn.trajectory {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.action-btn.context.active,
	.action-btn.trajectory.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.context-section,
	.trajectory-section {
		margin-top: var(--space-2);
		padding-top: var(--space-2);
		border-top: 1px solid var(--color-border);
	}

	.context-section :global(.context-panel) {
		background: transparent;
		border: none;
	}

	.context-section :global(.panel-header) {
		background: transparent;
		border-bottom: none;
		padding: 0 0 var(--space-2) 0;
	}

	.context-section :global(.panel-content) {
		padding: 0;
	}
</style>
