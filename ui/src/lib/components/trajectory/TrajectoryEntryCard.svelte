<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { TrajectoryEntry } from '$lib/types/trajectory';

	interface Props {
		entry: TrajectoryEntry;
		/** Applies tighter padding and smaller type. Does NOT gate expansion —
		 * expansion is always available when the entry has a preview payload. */
		compact?: boolean;
	}

	let { entry, compact = false }: Props = $props();

	let expanded = $state(false);

	const isModelCall = $derived(entry.step_type === 'model_call');
	const hasError = $derived(!!entry.error);
	const preview = $derived(entry.step_type === 'model_call' ? entry.response : entry.tool_result);
	// Audit fields surface the full conversation for model_call entries when
	// the backend records them (`trajectory_detail: full` on agentic-loop).
	// Without `messages`/`tool_calls` the trail is one-sided — token counts
	// without the prompt or response shape.
	const hasMessages = $derived(!!entry.messages && entry.messages.length > 0);
	const hasModelToolCalls = $derived(!!entry.tool_calls && entry.tool_calls.length > 0);
	const hasPreview = $derived(
		!!preview || !!entry.tool_arguments || hasMessages || hasModelToolCalls
	);

	function roleLabel(role: string): string {
		const labels: Record<string, string> = {
			system: 'system',
			user: 'user',
			assistant: 'assistant',
			tool: 'tool'
		};
		return labels[role] ?? role;
	}

	const displayName = $derived(
		entry.step_type === 'model_call'
			? (entry.model ?? entry.provider ?? 'Unknown model')
			: (entry.tool_name ?? 'Unknown tool')
	);

	const totalTokens = $derived(
		(entry.tokens_in ?? 0) + (entry.tokens_out ?? 0)
	);

	const utilization = $derived(entry.utilization ?? 0);
	const utilizationPct = $derived(Math.round(utilization * 100));
	const utilizationColor = $derived(
		utilization > 0.8 ? 'var(--color-error)' :
		utilization > 0.6 ? 'var(--color-warning)' :
		'var(--color-success)'
	);

	function formatDuration(ms: number | undefined): string {
		if (ms === undefined) return '—';
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(1)}s`;
	}

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function toggleExpanded() {
		if (!hasPreview) return;
		expanded = !expanded;
	}

	function handleCardClick(event: MouseEvent) {
		if (!hasPreview) return;
		const target = event.target as HTMLElement | null;
		if (target?.closest('button, a, input, textarea, select')) return;
		toggleExpanded();
	}

	function handleCardKeydown(event: KeyboardEvent) {
		if (!hasPreview) return;
		if (event.key !== 'Enter' && event.key !== ' ') return;
		event.preventDefault();
		toggleExpanded();
	}

</script>

<div
	class="entry-card"
	class:compact
	class:has-error={hasError}
	class:model-call={isModelCall}
	class:tool-call={!isModelCall}
	data-testid="trajectory-entry"
	data-step-type={entry.step_type}
	role="button"
	tabindex={hasPreview ? 0 : -1}
	aria-disabled={!hasPreview}
	aria-expanded={expanded}
	aria-label={`${expanded ? 'Collapse' : 'Expand'} trajectory entry ${displayName}`}
	onclick={handleCardClick}
	onkeydown={handleCardKeydown}
>
	<div class="card-header">
		<div class="header-left">
			<span class="type-badge" class:badge-model={isModelCall} class:badge-tool={!isModelCall}>
				{#if isModelCall}
					<Icon name="brain" size={10} />
					LLM
				{:else}
					<Icon name="wrench" size={10} />
					Tool
				{/if}
			</span>
			<span class="entry-name">{displayName}</span>
			{#if entry.retry_count && entry.retry_count > 0}
				<span class="retry-badge" title="Retried {entry.retry_count} time{entry.retry_count === 1 ? '' : 's'}">
					{entry.retry_count}x
				</span>
			{/if}
		</div>

		<div class="header-right">
			{#if hasError}
				<Icon name="alert-circle" size={14} class="error-icon" />
			{/if}
			{#if hasPreview}
				<!-- `compact` controls styling only (tighter padding/type);
					 expansion is always allowed when there's something to show.
					 Previously both were coupled, so the plan-page timeline
					 couldn't reveal prompt/response/tool args (bug #7.10). -->
				<span
					class="expand-btn"
					title={expanded ? 'Collapse' : 'Expand preview'}
					data-testid="entry-expand-btn"
					aria-hidden="true"
				>
					<Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={14} />
				</span>
			{/if}
		</div>
	</div>

	<div class="metrics-row">
		{#if isModelCall}
			{#if totalTokens > 0}
				<span class="metric">
					<Icon name="cpu" size={11} />
					{formatTokens(entry.tokens_in ?? 0)} / {formatTokens(entry.tokens_out ?? 0)} tokens
				</span>
			{/if}
			{#if utilization > 0}
				<span class="metric ctx-metric">
					<span
						class="ctx-bar"
						role="progressbar"
						aria-valuenow={utilizationPct}
						aria-valuemin={0}
						aria-valuemax={100}
						aria-label="Context window utilization: {utilizationPct}%"
					>
						<span class="ctx-fill" style="width: {utilizationPct}%; background: {utilizationColor}"></span>
					</span>
					<span class="ctx-label" style="color: {utilizationColor}">
						{utilizationPct}%
						{#if utilization > 0.8}<Icon name="alert-triangle" size={10} />{/if}
					</span>
				</span>
			{/if}
		{:else}
			<span class="metric">
				<Icon name="clock" size={11} />
				{formatDuration(entry.duration)}
			</span>
			{#if (entry.tokens_in ?? 0) > 0}
				<span class="metric">
					<Icon name="cpu" size={11} />
					{formatTokens(entry.tokens_in ?? 0)} tokens
				</span>
			{/if}
		{/if}
	</div>

	{#if hasError}
		<div class="error-message">
			<Icon name="alert-triangle" size={12} />
			<span>{entry.error}</span>
		</div>
	{/if}

	{#if expanded && hasPreview}
		<div class="preview-block" data-testid="entry-preview">
			{#if isModelCall && hasMessages}
				<div class="arguments-label">Request ({entry.messages!.length} message{entry.messages!.length === 1 ? '' : 's'})</div>
				<div class="messages-list">
					{#each entry.messages! as m, i (i)}
						<div class="message" data-role={m.role}>
							<span class="message-role message-role--{m.role}">{roleLabel(m.role)}</span>
							{#if m.content}
								<pre class="message-content">{m.content}</pre>
							{/if}
							{#if m.tool_calls && m.tool_calls.length > 0}
								<div class="message-tool-calls">
									{#each m.tool_calls as tc (tc.id)}
										<div class="message-tool-call">
											<span class="tc-name">{tc.name}</span>
											{#if tc.arguments}
												<pre class="tc-args">{JSON.stringify(tc.arguments, null, 2)}</pre>
											{/if}
										</div>
									{/each}
								</div>
							{/if}
							{#if m.tool_call_id}
								<div class="message-meta">tool_call_id: {m.tool_call_id}</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
			{#if isModelCall && (preview || hasModelToolCalls)}
				<div class="arguments-label">Response</div>
				{#if preview}
					<pre class="preview-text">{preview}</pre>
				{/if}
				{#if hasModelToolCalls}
					<div class="messages-list">
						{#each entry.tool_calls! as tc (tc.id)}
							<div class="message-tool-call">
								<span class="tc-name">{tc.name}</span>
								{#if tc.arguments}
									<pre class="tc-args">{JSON.stringify(tc.arguments, null, 2)}</pre>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			{/if}
			{#if !isModelCall && entry.tool_arguments}
				<div class="arguments-label">Arguments</div>
				<pre class="preview-text">{JSON.stringify(entry.tool_arguments, null, 2)}</pre>
			{/if}
			{#if !isModelCall && preview}
				<div class="arguments-label">Result</div>
				<pre class="preview-text">{preview}</pre>
			{/if}
		</div>
	{/if}
</div>

<style>
	.entry-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-3) var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		transition: border-color var(--transition-fast);
	}

	.entry-card[role='button']:not([aria-disabled='true']) {
		cursor: pointer;
	}

	.entry-card[role='button']:not([aria-disabled='true']):hover {
		border-color: var(--color-border-strong, var(--color-accent-muted));
	}

	.entry-card[role='button']:not([aria-disabled='true']):focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.entry-card.compact {
		padding: var(--space-2) var(--space-3);
		gap: var(--space-1);
	}

	.entry-card.has-error {
		border-color: var(--color-error);
		background: color-mix(in srgb, var(--color-error-muted) 30%, var(--color-bg-secondary));
	}

	.card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}

	.header-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
	}

	.header-right {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex-shrink: 0;
	}

	.type-badge {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 2px var(--space-2);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		border-radius: var(--radius-full);
		flex-shrink: 0;
		letter-spacing: 0.04em;
	}

	.badge-model {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.badge-tool {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.entry-name {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.compact .entry-name {
		font-size: var(--font-size-xs);
	}

	.retry-badge {
		display: inline-flex;
		align-items: center;
		padding: 1px var(--space-1);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		background: var(--color-warning-muted);
		color: var(--color-warning);
		border-radius: var(--radius-sm);
		flex-shrink: 0;
	}

	:global(.error-icon) {
		color: var(--color-error);
	}

	.expand-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
		transition: all var(--transition-fast);
		padding: 0;
	}

	.expand-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.metrics-row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-wrap: wrap;
	}

	.metric {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		font-family: var(--font-family-mono);
	}

	.ctx-metric {
		gap: var(--space-1);
	}

	.ctx-bar {
		display: inline-block;
		width: 48px;
		height: 6px;
		background: var(--color-bg-elevated);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.ctx-fill {
		display: block;
		height: 100%;
		border-radius: var(--radius-full);
		transition: width var(--transition-fast);
	}

	.ctx-label {
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
	}

	.error-message {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-error);
		padding: var(--space-1) var(--space-2);
		background: var(--color-error-muted);
		border-radius: var(--radius-sm);
	}

	.error-message span {
		line-height: var(--line-height-normal);
	}

	.preview-block {
		margin-top: var(--space-1);
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-2);
	}

	.arguments-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-bottom: var(--space-1);
	}

	.arguments-label + .preview-text + .arguments-label {
		margin-top: var(--space-2);
	}

	.preview-text {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		white-space: pre-wrap;
		word-break: break-word;
		line-height: var(--line-height-relaxed);
		margin: 0;
		max-height: 200px;
		overflow-y: auto;
	}

	/* Request audit trail — one message per row with a role chip + content.
	 * Each message scrolls internally so a 10k-char system prompt doesn't
	 * push everything else out of the viewport. */
	.messages-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-bottom: var(--space-2);
	}

	.message {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
	}

	.message-role {
		display: inline-flex;
		align-self: flex-start;
		padding: 1px var(--space-2);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		border-radius: var(--radius-full);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.message-role--system {
		background: var(--color-bg-elevated);
		color: var(--color-text-muted);
	}

	.message-role--user {
		background: color-mix(in srgb, var(--color-accent) 18%, transparent);
		color: var(--color-accent);
	}

	.message-role--assistant {
		background: color-mix(in srgb, var(--color-success) 18%, transparent);
		color: var(--color-success);
	}

	.message-role--tool {
		background: color-mix(in srgb, var(--color-warning) 18%, transparent);
		color: var(--color-warning);
	}

	.message-content {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		white-space: pre-wrap;
		word-break: break-word;
		line-height: var(--line-height-relaxed);
		margin: 0;
		max-height: 220px;
		overflow-y: auto;
	}

	.message-tool-calls {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.message-tool-call {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-sm);
	}

	.tc-name {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.tc-args {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		white-space: pre-wrap;
		word-break: break-word;
		line-height: var(--line-height-relaxed);
		margin: 0;
		max-height: 160px;
		overflow-y: auto;
	}

	.message-meta {
		font-family: var(--font-family-mono);
		font-size: 10px;
		color: var(--color-text-muted);
	}
</style>
