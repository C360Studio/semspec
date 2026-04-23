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
				<li class="picker-row">
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
