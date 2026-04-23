<script lang="ts">
	/**
	 * Cherry-pick which failed requirements to retry on a stalled plan.
	 *
	 * Today's "Retry Failed" button retries every failed/error requirement —
	 * fine when all failures share a root cause, but surgery-unfriendly when
	 * the user knows some failures are expected (e.g. a known bad-scope
	 * requirement they want to skip). This picker surfaces a checkbox per
	 * failed requirement so the user can pick exactly which to retry.
	 *
	 * Reuses the /plans/{slug}/branches endpoint for requirement metadata —
	 * the stage + branch info is already joined with EXECUTION_STATES there,
	 * so no new backend endpoint is required for the picker.
	 */
	import { fetchPlanBranches, type PlanRequirementBranch } from '$lib/api/branches';
	import { retrySelected } from '$lib/actions/plans';

	interface Props {
		slug: string;
		/** Called after a successful retry so the parent can refresh its view. */
		onRetried?: () => void;
	}

	let { slug, onRetried }: Props = $props();

	let branches = $state<PlanRequirementBranch[]>([]);
	let selectedIds = $state<Set<string>>(new Set());
	let expandedIds = $state<Set<string>>(new Set());
	let loading = $state(true);
	let submitting = $state(false);
	let error = $state<string | null>(null);

	const failed = $derived(
		branches.filter((b) => b.stage === 'failed' || b.stage === 'error')
	);

	const selectedCount = $derived(selectedIds.size);
	const canSubmit = $derived(!submitting && selectedCount > 0);

	$effect(() => {
		const currentSlug = slug;
		loading = true;
		error = null;
		let cancelled = false;
		fetchPlanBranches(currentSlug)
			.then((rows) => {
				if (cancelled) return;
				branches = rows;
			})
			.catch((e) => {
				if (!cancelled) {
					error = e instanceof Error ? e.message : 'Failed to load requirements';
				}
			})
			.finally(() => {
				if (!cancelled) loading = false;
			});
		return () => {
			cancelled = true;
		};
	});

	function toggle(id: string) {
		// Replace the Set so Svelte 5 picks up the mutation — Sets aren't deeply reactive.
		const next = new Set(selectedIds);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selectedIds = next;
	}

	function toggleDetails(id: string) {
		const next = new Set(expandedIds);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		expandedIds = next;
	}

	/**
	 * Summarize the "why" of a failure in one short line suitable for the
	 * row header. Users pick from the full text by expanding. Priority:
	 * review verdict + feedback (most actionable), then error reason, then
	 * a bare "failed" fallback.
	 */
	function summaryLine(b: PlanRequirementBranch): string {
		if (b.review_verdict || b.review_feedback) {
			const verdict = b.review_verdict ?? 'reviewer';
			const feedback = (b.review_feedback ?? '').replace(/\s+/g, ' ').trim();
			if (feedback.length > 0) {
				const clipped = feedback.length > 120 ? feedback.slice(0, 120) + '…' : feedback;
				return `${verdict}: ${clipped}`;
			}
			return verdict;
		}
		if (b.error_reason) {
			const reason = b.error_reason.replace(/\s+/g, ' ').trim();
			return reason.length > 160 ? reason.slice(0, 160) + '…' : reason;
		}
		return b.stage === 'error' ? 'Execution error (no detail)' : 'Failed (no detail)';
	}

	function hasDetails(b: PlanRequirementBranch): boolean {
		return Boolean(b.review_feedback || b.error_reason);
	}

	function retryBudgetText(b: PlanRequirementBranch): string | null {
		if (!b.retry_count && !b.max_retries) return null;
		const max = b.max_retries ?? 0;
		const used = b.retry_count ?? 0;
		if (max > 0) return `retry ${used}/${max}`;
		return `retry ${used}`;
	}

	/**
	 * True when the requirement used its entire retry budget — signals to the
	 * user that a bare retry will likely exhaust again. Picker surfaces a
	 * warning badge so the user considers a different strategy (swap model,
	 * skip, reject) instead of pounding retry.
	 */
	function isBudgetExhausted(b: PlanRequirementBranch): boolean {
		const max = b.max_retries ?? 0;
		const used = b.retry_count ?? 0;
		return max > 0 && used >= max;
	}

	function toggleAll() {
		if (selectedIds.size === failed.length) {
			selectedIds = new Set();
		} else {
			selectedIds = new Set(failed.map((r) => r.requirement_id));
		}
	}

	async function handleRetry() {
		if (!canSubmit) return;
		submitting = true;
		error = null;
		try {
			await retrySelected(slug, Array.from(selectedIds));
			selectedIds = new Set();
			onRetried?.();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Retry failed';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="retry-picker" data-testid="retry-selected-picker">
	{#if loading}
		<div class="picker-loading">Loading requirements&hellip;</div>
	{:else if error}
		<div class="picker-error" role="alert">{error}</div>
	{:else if failed.length === 0}
		<div class="picker-empty">No failed requirements to retry.</div>
	{:else}
		<div class="picker-header">
			<button
				type="button"
				class="select-all-btn"
				onclick={toggleAll}
				data-testid="retry-select-all"
			>
				{selectedIds.size === failed.length ? 'Clear all' : 'Select all'}
			</button>
			<span class="picker-count" data-testid="retry-selected-count">
				{selectedCount} of {failed.length} selected
			</span>
		</div>

		<ul class="picker-list" aria-label="Failed requirements">
			{#each failed as req (req.requirement_id)}
				{@const budget = retryBudgetText(req)}
				{@const exhausted = isBudgetExhausted(req)}
				{@const showDetailsBtn = hasDetails(req)}
				{@const isExpanded = expandedIds.has(req.requirement_id)}
				<li class="picker-row" data-testid="retry-row-{req.requirement_id}">
					<label class="picker-label">
						<input
							type="checkbox"
							checked={selectedIds.has(req.requirement_id)}
							onchange={() => toggle(req.requirement_id)}
							data-testid="retry-checkbox-{req.requirement_id}"
						/>
						<span class="req-id">[{req.requirement_id}]</span>
						<span class="req-title">{req.title}</span>
						<span class="req-stage req-stage--{req.stage}">{req.stage}</span>
					</label>
					<div class="req-context">
						<span
							class="req-summary"
							data-testid="retry-summary-{req.requirement_id}"
							title={req.review_feedback ?? req.error_reason ?? ''}
						>
							{summaryLine(req)}
						</span>
						{#if budget}
							<span
								class="req-budget"
								class:req-budget--exhausted={exhausted}
								data-testid="retry-budget-{req.requirement_id}"
							>
								{budget}
							</span>
						{/if}
						{#if exhausted}
							<span
								class="exhausted-badge"
								data-testid="retry-exhausted-{req.requirement_id}"
								title="This requirement has used its full retry budget. A plain retry is likely to exhaust again — consider swapping models, editing the requirement, or rejecting the plan."
							>
								Budget exhausted
							</span>
						{/if}
						{#if showDetailsBtn}
							<button
								type="button"
								class="details-btn"
								onclick={() => toggleDetails(req.requirement_id)}
								aria-expanded={isExpanded}
								data-testid="retry-details-btn-{req.requirement_id}"
							>
								{isExpanded ? 'Hide details' : 'Show details'}
							</button>
						{/if}
					</div>
					{#if isExpanded && showDetailsBtn}
						<div class="req-details" data-testid="retry-details-{req.requirement_id}">
							{#if req.review_feedback}
								<div class="detail-label">Reviewer feedback</div>
								<pre class="detail-body">{req.review_feedback}</pre>
							{/if}
							{#if req.error_reason}
								<div class="detail-label">Error reason</div>
								<pre class="detail-body">{req.error_reason}</pre>
							{/if}
						</div>
					{/if}
				</li>
			{/each}
		</ul>

		<div class="picker-actions">
			<button
				type="button"
				class="retry-btn"
				onclick={handleRetry}
				disabled={!canSubmit}
				aria-busy={submitting}
				data-testid="retry-submit"
			>
				{submitting ? 'Retrying…' : `Retry ${selectedCount} selected`}
			</button>
		</div>
	{/if}
</div>

<style>
	.retry-picker {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.picker-loading,
	.picker-empty {
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
		padding: var(--space-4);
	}

	.picker-error {
		color: var(--color-error);
		font-size: var(--font-size-sm);
		padding: var(--space-2);
	}

	.picker-header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		justify-content: space-between;
	}

	.select-all-btn {
		background: none;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: var(--space-1) var(--space-2);
		color: var(--color-text-primary);
		font-size: var(--font-size-xs);
		cursor: pointer;
	}

	.select-all-btn:hover {
		background: var(--color-bg-tertiary);
	}

	.picker-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.picker-list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		max-height: 320px;
		overflow-y: auto;
	}

	.picker-row {
		padding: var(--space-1) 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.req-context {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding-left: calc(var(--space-2) + 18px); /* align under title, past checkbox */
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.req-summary {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.req-budget {
		flex-shrink: 0;
		font-family: var(--font-family-mono);
		font-size: 10px;
		padding: 1px 5px;
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.req-budget--exhausted {
		background: color-mix(in srgb, var(--color-warning, var(--color-error)) 18%, transparent);
		color: var(--color-warning, var(--color-error));
	}

	.exhausted-badge {
		flex-shrink: 0;
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		padding: 1px 6px;
		border-radius: var(--radius-sm);
		text-transform: uppercase;
		letter-spacing: 0.03em;
		background: color-mix(in srgb, var(--color-warning, var(--color-error)) 15%, transparent);
		color: var(--color-warning, var(--color-error));
	}

	.details-btn {
		flex-shrink: 0;
		background: none;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		padding: 1px var(--space-2);
		color: var(--color-text-secondary);
		font-size: 10px;
		cursor: pointer;
	}

	.details-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.req-details {
		margin-left: calc(var(--space-2) + 18px);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.detail-label {
		font-size: 10px;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.03em;
		margin-top: var(--space-1);
	}

	.detail-body {
		margin: 0;
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
		white-space: pre-wrap;
		word-break: break-word;
		max-height: 240px;
		overflow-y: auto;
	}

	.picker-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		cursor: pointer;
		font-size: var(--font-size-sm);
	}

	.picker-label input {
		flex-shrink: 0;
	}

	.req-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-accent);
		flex-shrink: 0;
	}

	.req-title {
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--color-text-primary);
	}

	.req-stage {
		flex-shrink: 0;
		font-size: 10px;
		padding: 1px 5px;
		border-radius: var(--radius-sm);
		text-transform: uppercase;
		letter-spacing: 0.03em;
	}

	.req-stage--failed,
	.req-stage--error {
		background: color-mix(in srgb, var(--color-error) 15%, transparent);
		color: var(--color-error);
	}

	.picker-actions {
		display: flex;
		justify-content: flex-end;
	}

	.retry-btn {
		background: var(--color-success);
		color: white;
		border: none;
		border-radius: var(--radius-sm);
		padding: var(--space-2) var(--space-4);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
	}

	.retry-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.retry-btn:not(:disabled):hover {
		filter: brightness(1.1);
	}
</style>
