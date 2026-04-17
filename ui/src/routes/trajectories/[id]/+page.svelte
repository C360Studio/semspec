<script lang="ts">
	/**
	 * Trajectory Timeline View — step-by-step timeline for an agent loop.
	 *
	 * Left panel: step index / navigator with type counts and summary metrics
	 * Center panel: chronological timeline of trajectory steps
	 * Right panel: context metadata (collapsed by default)
	 */

	import { invalidate } from '$app/navigation';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import TrajectoryEntryCard from '$lib/components/trajectory/TrajectoryEntryCard.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { TrajectoryEntry } from '$lib/types/trajectory';

	interface Props {
		data: {
			loopId: string;
			trajectory: import('$lib/types/trajectory').Trajectory | null;
			loop: import('$lib/types').Loop | null;
		};
	}

	let { data }: Props = $props();

	const loopId = $derived(data.loopId);
	const loop = $derived(data.loop);
	const trajectory = $derived(data.trajectory);
	const loading = false; // Load function handles fetching before render
	const error = $derived(trajectory === null && data.loopId ? 'Failed to load trajectory' : null);
	const entries = $derived(trajectory?.steps ?? []);

	// Step type index
	const modelCalls = $derived(entries.filter((e) => e.step_type === 'model_call'));
	const toolCalls = $derived(entries.filter((e) => e.step_type === 'tool_call'));
	const errorEntries = $derived(entries.filter((e) => !!e.error));

	// Summary metrics
	const totalTokensIn = $derived(entries.reduce((s, e) => s + (e.tokens_in ?? 0), 0));
	const totalTokensOut = $derived(entries.reduce((s, e) => s + (e.tokens_out ?? 0), 0));
	const totalDurationMs = $derived(entries.reduce((s, e) => s + (e.duration ?? 0), 0));
	const peakUtilization = $derived(Math.max(0, ...entries.map(e => e.utilization ?? 0)));
	const peakUtilizationPct = $derived(Math.round(peakUtilization * 100));

	// Expanded step tracking
	let expandedIndices = $state<Set<number>>(new Set());
	let activeSection = $state<string>('all');

	// Filter entries by active section
	const visibleEntries = $derived.by(() => {
		if (activeSection === 'model') return entries.map((e: TrajectoryEntry, i: number) => ({ e, i })).filter(({ e }) => e.step_type === 'model_call');
		if (activeSection === 'tool') return entries.map((e: TrajectoryEntry, i: number) => ({ e, i })).filter(({ e }) => e.step_type === 'tool_call');
		if (activeSection === 'errors') return entries.map((e: TrajectoryEntry, i: number) => ({ e, i })).filter(({ e }) => !!e.error);
		return entries.map((e: TrajectoryEntry, i: number) => ({ e, i }));
	});

	function toggleExpanded(index: number) {
		const next = new Set(expandedIndices);
		if (next.has(index)) next.delete(index);
		else next.add(index);
		expandedIndices = next;
	}

	function expandAll() {
		expandedIndices = new Set(entries.map((_: TrajectoryEntry, i: number) => i));
	}

	function collapseAll() {
		expandedIndices = new Set();
	}

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60_000).toFixed(1)}m`;
	}

	function getStatusClass(state: string): string {
		if (state === 'complete') return 'status-success';
		if (state === 'failed' || state === 'error') return 'status-error';
		if (state === 'executing' || state === 'pending') return 'status-running';
		return 'status-muted';
	}

	function handleRefresh() {
		invalidate('url');
	}
</script>

<svelte:head>
	<title>Trajectory {loopId.slice(0, 8)} - SemSpec</title>
</svelte:head>

<ThreePanelLayout id="trajectory-detail" leftOpen={true} rightOpen={false}>
	{#snippet leftPanel()}
		<div class="step-index-panel">
			<div class="index-header">
				<span class="index-title">Steps</span>
			</div>

			<!-- Summary metrics -->
			{#if trajectory && entries.length > 0}
				<div class="metrics-section">
					<div class="metric-row">
						<Icon name="list" size={13} />
						<span>{entries.length} total steps</span>
					</div>
					{#if totalTokensIn > 0 || totalTokensOut > 0}
						<div class="metric-row">
							<Icon name="cpu" size={13} />
							<span>{formatTokens(totalTokensIn + totalTokensOut)} tokens</span>
						</div>
					{/if}
					{#if totalDurationMs > 0}
						<div class="metric-row">
							<Icon name="clock" size={13} />
							<span>{formatDuration(totalDurationMs)}</span>
						</div>
					{/if}
					{#if peakUtilization > 0}
						<div class="metric-row" style="color: {peakUtilization > 0.8 ? 'var(--color-error)' : peakUtilization > 0.6 ? 'var(--color-warning)' : 'var(--color-text-secondary)'}">
							{#if peakUtilization > 0.8}<Icon name="alert-triangle" size={13} />{:else}<Icon name="gauge" size={13} />{/if}
							<span>Peak ctx: {peakUtilizationPct}%</span>
						</div>
					{/if}
				</div>
			{/if}

			<!-- Step type navigator -->
			<div class="nav-section">
				<button
					class="nav-btn"
					class:active={activeSection === 'all'}
					onclick={() => (activeSection = 'all')}
				>
					<Icon name="list" size={14} />
					<span>All Steps</span>
					<span class="nav-count">{entries.length}</span>
				</button>
				{#if modelCalls.length > 0}
					<button
						class="nav-btn"
						class:active={activeSection === 'model'}
						onclick={() => (activeSection = 'model')}
					>
						<Icon name="brain" size={14} />
						<span>Model Calls</span>
						<span class="nav-count">{modelCalls.length}</span>
					</button>
				{/if}
				{#if toolCalls.length > 0}
					<button
						class="nav-btn"
						class:active={activeSection === 'tool'}
						onclick={() => (activeSection = 'tool')}
					>
						<Icon name="wrench" size={14} />
						<span>Tool Calls</span>
						<span class="nav-count">{toolCalls.length}</span>
					</button>
				{/if}
				{#if errorEntries.length > 0}
					<button
						class="nav-btn nav-btn-error"
						class:active={activeSection === 'errors'}
						onclick={() => (activeSection = 'errors')}
					>
						<Icon name="alert-triangle" size={14} />
						<span>Errors</span>
						<span class="nav-count">{errorEntries.length}</span>
					</button>
				{/if}
			</div>
		</div>
	{/snippet}

	{#snippet centerPanel()}
		<div class="trajectory-page" data-testid="trajectory-detail-page">
			<!-- Back link + header -->
			<header class="page-header">
				<a href="/trajectories" class="back-link" data-testid="trajectory-back-link">
					<Icon name="arrow-left" size={14} />
					Back to Trajectories
				</a>
			</header>

			<div class="trajectory-header">
				<h1 data-testid="trajectory-heading">Trajectory Timeline</h1>
				<div class="header-id-row">
					<code class="trajectory-id" data-testid="trajectory-id">{loopId}</code>
					{#if loop}
						<span class="status-badge {getStatusClass(loop.state)}">{loop.state}</span>
					{/if}
				</div>
				{#if loop?.task_id}
					<div class="source-context">
						<Icon name="git-pull-request" size={14} />
						<span class="source-task">{loop.task_id}</span>
						{#if loop.workflow_slug}
							<span class="source-workflow">{loop.workflow_slug}</span>
						{/if}
					</div>
				{/if}
			</div>

			<!-- Trajectory summary bar -->
			{#if trajectory && (totalTokensIn > 0 || totalTokensOut > 0 || entries.length > 0)}
				<div class="summary-bar" data-testid="trajectory-summary">
					{#if trajectory.steps.length > 0}
						<span class="summary-item">
							<strong>{trajectory.steps.length}</strong> steps
						</span>
					{/if}
					{#if totalTokensIn > 0 || totalTokensOut > 0}
						<span class="summary-item" data-testid="trajectory-tokens">
							<strong>{formatTokens(totalTokensIn)}</strong> in /
							<strong>{formatTokens(totalTokensOut)}</strong> out tokens
						</span>
					{/if}
					{#if trajectory.duration > 0}
						<span class="summary-item" data-testid="trajectory-duration">
							<strong>{formatDuration(trajectory.duration)}</strong>
						</span>
					{/if}
					{#if peakUtilization > 0}
						<span
							class="summary-item"
							data-testid="trajectory-peak-ctx"
							style="color: {peakUtilization > 0.8 ? 'var(--color-error)' : peakUtilization > 0.6 ? 'var(--color-warning)' : 'inherit'}"
						>
							{#if peakUtilization > 0.8}<Icon name="alert-triangle" size={13} />{/if}
							Peak ctx: <strong>{peakUtilizationPct}%</strong>
						</span>
					{/if}
					{#if entries.length > 0}
						<span class="summary-actions">
							<button class="text-btn" onclick={expandAll}>Expand all</button>
							<button class="text-btn" onclick={collapseAll}>Collapse all</button>
							<button
								class="text-btn"
								onclick={handleRefresh}
								disabled={loading}
								title="Refresh"
							>
								<Icon name="refresh-cw" size={12} class={loading ? 'spin' : ''} />
							</button>
						</span>
					{/if}
				</div>
			{/if}

			<!-- Loading / error / empty states -->
			{#if loading && !trajectory}
				<div class="loading-state" data-testid="trajectory-loading">
					<p>Loading trajectory...</p>
				</div>
			{:else if error}
				<div class="error-state" data-testid="trajectory-error">
					<Icon name="alert-triangle" size={20} />
					<p>{error}</p>
					<button class="btn btn-secondary btn-sm" onclick={handleRefresh}>Retry</button>
				</div>
			{:else if !trajectory}
				<div class="empty-state" data-testid="trajectory-not-found">
					<Icon name="history" size={28} />
					<p>No trajectory data found</p>
					<span class="empty-hint">The loop may still be running or data has not been recorded yet</span>
					<button class="btn btn-secondary btn-sm" onclick={handleRefresh}>Check again</button>
				</div>
			{:else if visibleEntries.length === 0}
				<div class="empty-state" data-testid="trajectory-empty-steps">
					<Icon name="history" size={28} />
					<p>No steps recorded yet</p>
					<span class="empty-hint">Steps will appear here as the agent runs</span>
				</div>
			{:else}
				<!-- Timeline -->
				<div class="timeline" data-testid="trajectory-timeline">
					{#each visibleEntries as { e: entry, i: index } (index)}
						<div class="timeline-event" data-testid="timeline-event" data-step-type={entry.step_type}>
							<div class="event-connector">
								<div class="event-dot" data-step-type={entry.step_type}></div>
								{#if index < entries.length - 1}
									<div class="connector-line"></div>
								{/if}
							</div>
							<div class="event-card">
								<TrajectoryEntryCard {entry} />
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{/snippet}

	{#snippet rightPanel()}
		<div class="context-panel">
			<div class="context-header">
				<span class="context-title">Context</span>
			</div>
			<div class="context-content">
				{#if loop}
					<section class="context-section">
						<h3 class="section-heading">Loop Info</h3>
						<dl class="prop-list">
							<dt>Loop ID</dt>
							<dd><code>{loop.loop_id}</code></dd>

							<dt>State</dt>
							<dd>
								<span class="status-badge {getStatusClass(loop.state)}">{loop.state}</span>
							</dd>

							{#if loop.task_id}
								<dt>Task</dt>
								<dd><code class="mono-value">{loop.task_id}</code></dd>
							{/if}

							{#if loop.workflow_slug}
								<dt>Workflow</dt>
								<dd><code class="mono-value">{loop.workflow_slug}</code></dd>
							{/if}

							{#if loop.workflow_step}
								<dt>Step</dt>
								<dd>{loop.workflow_step}</dd>
							{/if}

							{#if loop.iterations !== undefined}
								<dt>Iterations</dt>
								<dd>{loop.iterations} / {loop.max_iterations}</dd>
							{/if}

							{#if loop.created_at}
								<dt>Started</dt>
								<dd>{new Date(loop.created_at).toLocaleString()}</dd>
							{/if}

							{#if loop.completed_at}
								<dt>Completed</dt>
								<dd>{new Date(loop.completed_at).toLocaleString()}</dd>
							{/if}

							{#if loop.outcome}
								<dt>Outcome</dt>
								<dd>{loop.outcome}</dd>
							{/if}

							{#if loop.error}
								<dt>Error</dt>
								<dd class="error-value">{loop.error}</dd>
							{/if}
						</dl>
					</section>
				{/if}

				{#if trajectory}
					<section class="context-section">
						<h3 class="section-heading">Metrics</h3>
						<dl class="prop-list">
							<dt>Steps</dt>
							<dd>{trajectory.steps.length}</dd>
							<dt>Model Calls</dt>
							<dd>{trajectory.steps.filter((s) => s.step_type === 'model_call').length}</dd>
							<dt>Tool Calls</dt>
							<dd>{trajectory.steps.filter((s) => s.step_type === 'tool_call').length}</dd>
							{#if trajectory.total_tokens_in > 0}
								<dt>Tokens In</dt>
								<dd>{trajectory.total_tokens_in.toLocaleString()}</dd>
							{/if}
							{#if trajectory.total_tokens_out > 0}
								<dt>Tokens Out</dt>
								<dd>{trajectory.total_tokens_out.toLocaleString()}</dd>
							{/if}
							{#if trajectory.duration > 0}
								<dt>Duration</dt>
								<dd>{formatDuration(trajectory.duration)}</dd>
							{/if}
						</dl>
					</section>
				{/if}

				{#if !loop && !trajectory}
					<p class="empty-hint">No context data available</p>
				{/if}
			</div>
		</div>
	{/snippet}
</ThreePanelLayout>

<style>
	/* ---- Left panel: step index ---- */
	.step-index-panel {
		height: 100%;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
	}

	.index-header {
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.index-title {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--color-text-muted);
	}

	.metrics-section {
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		flex-shrink: 0;
	}

	.metric-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		font-family: var(--font-family-mono);
	}

	.nav-section {
		padding: var(--space-2) var(--space-2);
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.nav-btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-2);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		text-align: left;
		transition: all var(--transition-fast);
		width: 100%;
	}

	.nav-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.nav-btn.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.nav-btn-error.active {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.nav-count {
		margin-left: auto;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
		flex-shrink: 0;
	}

	.nav-btn.active .nav-count {
		color: var(--color-accent);
	}

	.nav-btn-error.active .nav-count {
		color: var(--color-error);
	}

	/* ---- Center panel ---- */
	.trajectory-page {
		height: 100%;
		overflow-y: auto;
		padding: var(--space-6);
	}

	.page-header {
		margin-bottom: var(--space-4);
	}

	.back-link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		text-decoration: none;
		transition: color var(--transition-fast);
	}

	.back-link:hover {
		color: var(--color-text-primary);
		text-decoration: none;
	}

	.trajectory-header {
		margin-bottom: var(--space-5);
	}

	.trajectory-header h1 {
		margin: 0 0 var(--space-2);
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.header-id-row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-2);
	}

	.trajectory-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		word-break: break-all;
	}

	.source-context {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.source-task {
		color: var(--color-text-primary);
	}

	.source-workflow {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		background: var(--color-bg-tertiary);
		padding: 1px var(--space-2);
		border-radius: var(--radius-sm);
	}

	/* Summary bar */
	.summary-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-4);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		margin-bottom: var(--space-5);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.summary-item strong {
		color: var(--color-text-primary);
	}

	.summary-actions {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.text-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		background: none;
		border: none;
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
		cursor: pointer;
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		transition: all var(--transition-fast);
	}

	.text-btn:hover:not(:disabled) {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}

	.text-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* Timeline */
	.timeline {
		display: flex;
		flex-direction: column;
	}

	.timeline-event {
		display: flex;
		gap: var(--space-3);
	}

	.event-connector {
		display: flex;
		flex-direction: column;
		align-items: center;
		flex-shrink: 0;
		padding-top: 14px;
	}

	.event-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: var(--color-accent);
		flex-shrink: 0;
	}

	.event-dot[data-step-type='tool_call'] {
		background: var(--color-warning);
	}

	.connector-line {
		width: 2px;
		flex: 1;
		background: var(--color-border);
		margin-top: 2px;
		min-height: var(--space-2);
	}

	.event-card {
		flex: 1;
		min-width: 0;
		padding-bottom: var(--space-3);
	}

	/* Status badges */
	.status-badge {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		text-transform: capitalize;
		flex-shrink: 0;
	}

	.status-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.status-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.status-running {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.status-muted {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	/* Loading / error / empty states */
	.loading-state,
	.error-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-12) var(--space-6);
		text-align: center;
		color: var(--color-text-muted);
	}

	.loading-state p,
	.error-state p,
	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.error-state {
		color: var(--color-error);
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-md);
		border: 1px solid transparent;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-sm {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-secondary {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
		border-color: var(--color-border);
	}

	.btn-secondary:hover {
		background: var(--color-bg-elevated);
	}

	/* ---- Right panel: context metadata ---- */
	.context-panel {
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.context-header {
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		flex-shrink: 0;
	}

	.context-title {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--color-text-muted);
	}

	.context-content {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-4);
	}

	.context-section {
		margin-bottom: var(--space-5);
	}

	.context-section:last-child {
		margin-bottom: 0;
	}

	.section-heading {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--color-text-muted);
		margin: 0 0 var(--space-3);
	}

	.prop-list {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
		margin: 0;
	}

	.prop-list dt {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		white-space: nowrap;
		padding-top: 2px;
	}

	.prop-list dd {
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		margin: 0;
		word-break: break-all;
	}

	.prop-list dd code {
		font-family: var(--font-family-mono);
		font-size: 11px;
		color: var(--color-text-secondary);
		word-break: break-all;
	}

	.mono-value {
		font-family: var(--font-family-mono);
		font-size: 11px;
	}

	.error-value {
		color: var(--color-error);
	}

	.empty-hint {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		text-align: center;
		margin: var(--space-6) 0 0;
	}

</style>
