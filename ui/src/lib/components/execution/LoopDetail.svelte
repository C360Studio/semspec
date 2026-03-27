<script lang="ts">
	/**
	 * LoopDetail — Agent loop detail slide-in panel.
	 *
	 * Displays loop metadata (role, model, status, duration), tool calls,
	 * spawned children, and token usage summary.
	 * Fetches trajectory data locally via api.trajectory.getByLoop.
	 */

	import Icon from '../shared/Icon.svelte';
	import TrajectoryEntryCard from '../trajectory/TrajectoryEntryCard.svelte';
	import { api } from '$lib/api/client';
	import type { Trajectory } from '$lib/types/trajectory';
	import type { AgentLoop } from '$lib/types/execution';
	import { getAgentLoopStatusClass } from '$lib/types/execution';

	interface Props {
		loop: AgentLoop;
		/** Callback to close the detail panel */
		onClose: () => void;
		/** Pre-loaded trajectory — skips client fetch when provided */
		initialTrajectory?: Trajectory | null;
	}

	let { loop, onClose, initialTrajectory = null }: Props = $props();

	let trajectory = $state<Trajectory | null>(initialTrajectory);
	let trajectoryLoading = $state(false);
	let trajectoryError = $state<string | null>(null);

	function fetchTrajectory(loopId: string) {
		trajectoryLoading = true;
		trajectoryError = null;
		api.trajectory.getByLoop(loopId)
			.then((t) => { trajectory = t; })
			.catch((e) => { trajectoryError = e instanceof Error ? e.message : 'Failed to fetch trajectory'; })
			.finally(() => { trajectoryLoading = false; });
	}

	// Only fetch if no initial data provided
	$effect(() => {
		if (!initialTrajectory) {
			const id = loop.loopId;
			if (id) fetchTrajectory(id);
		}
	});
	const entries = $derived(trajectory?.steps ?? []);

	const toolCalls = $derived(entries.filter((e) => e.step_type === 'tool_call'));
	const modelCalls = $derived(entries.filter((e) => e.step_type === 'model_call'));

	const totalTokensIn = $derived(entries.reduce((sum, e) => sum + (e.tokens_in ?? 0), 0));
	const totalTokensOut = $derived(entries.reduce((sum, e) => sum + (e.tokens_out ?? 0), 0));

	const statusClass = $derived(getAgentLoopStatusClass(loop.status));

	function formatRole(role: string): string {
		return role
			.split(/[-_]/)
			.map((w) => w.charAt(0).toUpperCase() + w.slice(1))
			.join(' ');
	}

	function formatModel(model: string): string {
		const parts = model.split('/');
		return parts[parts.length - 1];
	}

	function formatDuration(startedAt?: string, completedAt?: string): string {
		if (!startedAt) return '—';
		const end = completedAt ? new Date(completedAt) : new Date();
		const start = new Date(startedAt);
		const ms = end.getTime() - start.getTime();
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
		return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`;
	}

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatTimestamp(ts?: string): string {
		if (!ts) return '—';
		return new Date(ts).toLocaleTimeString();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') onClose();
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- Backdrop -->
<div
	class="detail-backdrop"
	onclick={onClose}
	onkeydown={(e) => e.key === 'Enter' && onClose()}
	role="presentation"
></div>

<!-- Slide-in panel -->
<aside class="detail-panel" aria-label="Loop details for {formatRole(loop.role)}">
	<div class="panel-header">
		<div class="header-info">
			<span class="status-indicator status-{statusClass}" aria-hidden="true"></span>
			<h2 class="panel-title">{formatRole(loop.role)}</h2>
			<span class="status-badge badge-{statusClass}">{loop.status}</span>
		</div>
		<button class="close-btn" onclick={onClose} aria-label="Close details">
			<Icon name="x" size={20} />
		</button>
	</div>

	<div class="panel-body">
		<!-- Metadata section -->
		<section class="meta-section" aria-label="Loop metadata">
			<dl class="meta-grid">
				<div class="meta-item">
					<dt>Loop ID</dt>
					<dd class="mono">{loop.loopId}</dd>
				</div>
				<div class="meta-item">
					<dt>Model</dt>
					<dd class="mono">{formatModel(loop.model)}</dd>
				</div>
				<div class="meta-item">
					<dt>Depth</dt>
					<dd>{loop.depth}</dd>
				</div>
				<div class="meta-item">
					<dt>Duration</dt>
					<dd>{formatDuration(loop.startedAt, loop.completedAt)}</dd>
				</div>
				{#if loop.startedAt}
					<div class="meta-item">
						<dt>Started</dt>
						<dd>{formatTimestamp(loop.startedAt)}</dd>
					</div>
				{/if}
				{#if loop.completedAt}
					<div class="meta-item">
						<dt>Completed</dt>
						<dd>{formatTimestamp(loop.completedAt)}</dd>
					</div>
				{/if}
				{#if loop.taskId}
					<div class="meta-item">
						<dt>Task</dt>
						<dd class="mono">{loop.taskId}</dd>
					</div>
				{/if}
				{#if loop.parentLoopId}
					<div class="meta-item">
						<dt>Parent</dt>
						<dd class="mono">{loop.parentLoopId.slice(0, 12)}</dd>
					</div>
				{/if}
			</dl>
		</section>

		<!-- Token usage summary -->
		{#if totalTokensIn + totalTokensOut > 0}
			<section class="token-section" aria-label="Token usage">
				<h3 class="section-title">
					<Icon name="cpu" size={14} />
					Token Usage
				</h3>
				<div class="token-grid">
					<div class="token-stat">
						<span class="token-label">Input</span>
						<span class="token-value">{formatTokens(totalTokensIn)}</span>
					</div>
					<div class="token-stat">
						<span class="token-label">Output</span>
						<span class="token-value">{formatTokens(totalTokensOut)}</span>
					</div>
					<div class="token-stat total">
						<span class="token-label">Total</span>
						<span class="token-value">{formatTokens(totalTokensIn + totalTokensOut)}</span>
					</div>
					{#if modelCalls.length > 0}
						<div class="token-stat">
							<span class="token-label">LLM Calls</span>
							<span class="token-value">{modelCalls.length}</span>
						</div>
					{/if}
				</div>
			</section>
		{/if}

		<!-- Spawned children -->
		{#if loop.children.length > 0}
			<section class="children-section" aria-label="Spawned child loops">
				<h3 class="section-title">
					<Icon name="git-branch" size={14} />
					Spawned Loops ({loop.children.length})
				</h3>
				<ul class="children-list">
					{#each loop.children as child (child.loopId)}
						{@const childStatusClass = getAgentLoopStatusClass(child.status)}
						<li class="child-item">
							<span class="status-dot dot-{childStatusClass}" aria-hidden="true"></span>
							<span class="child-role">{formatRole(child.role)}</span>
							<span class="child-model">{formatModel(child.model)}</span>
							<span class="child-badge badge-{childStatusClass}">{child.status}</span>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<!-- Tool calls -->
		{#if toolCalls.length > 0}
			<section class="tools-section" aria-label="Tool calls">
				<h3 class="section-title">
					<Icon name="wrench" size={14} />
					Tool Calls ({toolCalls.length})
				</h3>
				<ul class="entry-list">
					{#each toolCalls as entry, i (i)}
						<li>
							<TrajectoryEntryCard {entry} compact={true} />
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<!-- Trajectory / LLM calls -->
		{#if trajectoryLoading && entries.length === 0}
			<div class="loading-state">
				<Icon name="loader" size={20} class="spin" />
				<span>Loading trajectory...</span>
			</div>
		{:else if trajectoryError}
			<div class="error-state">
				<Icon name="alert-triangle" size={16} />
				<span>{trajectoryError}</span>
				<button
					class="retry-btn"
					onclick={() => fetchTrajectory(loop.loopId)}
				>Retry</button>
			</div>
		{:else if entries.length === 0 && !trajectoryLoading}
			<div class="empty-state">
				<Icon name="history" size={20} />
				<span>No trajectory data</span>
			</div>
		{/if}
	</div>
</aside>

<style>
	.detail-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.4);
		z-index: calc(var(--z-modal, 100) - 1);
	}

	.detail-panel {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(480px, 100vw);
		background: var(--color-bg-secondary);
		border-left: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		z-index: var(--z-modal, 100);
		overflow: hidden;
		box-shadow: -4px 0 24px rgba(0, 0, 0, 0.3);
		animation: slide-in 0.2s ease-out;
	}

	@keyframes slide-in {
		from { transform: translateX(100%); }
		to { transform: translateX(0); }
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		gap: var(--space-3);
		flex-shrink: 0;
	}

	.header-info {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
	}

	.status-indicator {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.status-indicator.status-warning { background: var(--color-warning); }
	.status-indicator.status-success { background: var(--color-success); }
	.status-indicator.status-error { background: var(--color-error); }

	.panel-title {
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.status-badge {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 2px 8px;
		border-radius: var(--radius-full);
		white-space: nowrap;
		flex-shrink: 0;
	}

	.badge-warning { background: var(--color-warning-muted); color: var(--color-warning); }
	.badge-success { background: var(--color-success-muted); color: var(--color-success); }
	.badge-error { background: var(--color-error-muted); color: var(--color-error); }
	.badge-neutral { background: var(--color-bg-elevated); color: var(--color-text-secondary); }

	.close-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		background: transparent;
		border: none;
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		cursor: pointer;
		flex-shrink: 0;
		transition: all var(--transition-fast);
	}

	.close-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.panel-body {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 0;
	}

	.meta-section,
	.token-section,
	.children-section,
	.tools-section {
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.section-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0 0 var(--space-3) 0;
	}

	.meta-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2) var(--space-4);
		margin: 0;
	}

	.meta-item dt {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-weight: var(--font-weight-medium);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-bottom: 2px;
	}

	.meta-item dd {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		margin: 0;
		word-break: break-all;
	}

	.meta-item dd.mono {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
	}

	.token-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: var(--space-2);
	}

	.token-stat {
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
	}

	.token-stat.total {
		background: var(--color-accent-muted);
	}

	.token-label {
		font-size: 10px;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.token-value {
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
	}

	.token-stat.total .token-value {
		color: var(--color-accent);
	}

	.children-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.child-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.dot-warning { background: var(--color-warning); }
	.dot-success { background: var(--color-success); }
	.dot-error { background: var(--color-error); }
	.dot-neutral { background: var(--color-text-muted); }

	.child-role {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		flex: 1;
	}

	.child-model {
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.child-badge {
		font-size: 10px;
		padding: 1px 6px;
		border-radius: var(--radius-full);
	}

	.entry-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.loading-state,
	.error-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.error-state {
		color: var(--color-error);
	}

	.retry-btn {
		padding: var(--space-1) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
		cursor: pointer;
		color: var(--color-text-primary);
	}

	.retry-btn:hover {
		background: var(--color-bg-elevated);
	}

</style>
