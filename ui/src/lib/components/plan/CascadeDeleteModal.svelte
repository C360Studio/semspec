<script lang="ts">
	/**
	 * CascadeDeleteModal — Confirmation modal for deprecating a requirement.
	 *
	 * Walks the depends_on[] graph client-side to show the full blast radius:
	 * all dependent requirements and their scenarios that will also be deprecated.
	 */

	import Modal from '$lib/components/shared/Modal.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { Requirement } from '$lib/types/requirement';
	import type { Scenario } from '$lib/types/scenario';

	interface Props {
		/** The requirement being deprecated */
		requirement: Requirement;
		/** All requirements in the plan (for dependency walking) */
		allRequirements: Requirement[];
		/** Scenarios keyed by requirement ID */
		scenariosByReq: Record<string, Scenario[]>;
		/** Called with all affected requirement IDs on confirm */
		onConfirm: (affectedIds: string[]) => Promise<void>;
		onClose: () => void;
	}

	let { requirement, allRequirements, scenariosByReq, onConfirm, onClose }: Props = $props();

	let confirming = $state(false);

	// Walk depends_on[] to find all requirements that transitively depend on this one
	const affectedRequirements = $derived.by(() => {
		const affected = new Set<string>();
		const queue = [requirement.id];

		while (queue.length > 0) {
			const current = queue.shift()!;
			for (const req of allRequirements) {
				if (req.status !== 'active') continue;
				if (affected.has(req.id)) continue;
				if (req.depends_on?.includes(current)) {
					affected.add(req.id);
					queue.push(req.id);
				}
			}
		}

		return allRequirements.filter((r) => affected.has(r.id));
	});

	// Count scenarios that will be affected
	const affectedScenarioCount = $derived.by(() => {
		const reqIds = [requirement.id, ...affectedRequirements.map((r) => r.id)];
		return reqIds.reduce((sum, id) => sum + (scenariosByReq[id]?.length ?? 0), 0);
	});

	const allAffectedIds = $derived([requirement.id, ...affectedRequirements.map((r) => r.id)]);
	const hasDependents = $derived(affectedRequirements.length > 0 || affectedScenarioCount > 0);

	async function handleConfirm() {
		confirming = true;
		try {
			await onConfirm(allAffectedIds);
			onClose();
		} finally {
			confirming = false;
		}
	}
</script>

<Modal title="Deprecate Requirement" {onClose}>
	<div class="cascade-modal">
		<div class="target-requirement">
			<Icon name="minus-circle" size={16} />
			<span class="target-title">{requirement.title}</span>
		</div>

		{#if hasDependents}
			<div class="cascade-warning" role="alert">
				<Icon name="alert-triangle" size={16} />
				<span>This will also deprecate the following:</span>
			</div>

			{#if affectedRequirements.length > 0}
				<div class="affected-section">
					<h4 class="affected-heading">
						<Icon name="list-checks" size={14} />
						{affectedRequirements.length} dependent requirement{affectedRequirements.length !== 1 ? 's' : ''}
					</h4>
					<ul class="affected-list" role="list">
						{#each affectedRequirements as req (req.id)}
							<li class="affected-item">
								<Icon name="arrow-right" size={12} />
								<span>{req.title}</span>
								{#if scenariosByReq[req.id]?.length}
									<span class="scenario-count">({scenariosByReq[req.id].length} scenarios)</span>
								{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if affectedScenarioCount > 0}
				<div class="affected-summary">
					<Icon name="test-tube" size={14} />
					<span>{affectedScenarioCount} scenario{affectedScenarioCount !== 1 ? 's' : ''} will be removed</span>
				</div>
			{/if}
		{:else}
			<p class="no-dependents">No other requirements depend on this one.</p>
		{/if}

		<div class="modal-actions">
			<button
				class="btn btn-ghost"
				onclick={onClose}
				disabled={confirming}
			>
				Cancel
			</button>
			<button
				class="btn btn-danger"
				onclick={handleConfirm}
				disabled={confirming}
				aria-busy={confirming}
			>
				{#if confirming}
					Deprecating...
				{:else if hasDependents}
					Deprecate All ({allAffectedIds.length})
				{:else}
					Deprecate
				{/if}
			</button>
		</div>
	</div>
</Modal>

<style>
	.cascade-modal {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.target-requirement {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-weight: var(--font-weight-medium);
		font-size: var(--font-size-sm);
	}

	.target-requirement :global(svg) {
		color: var(--color-error);
		flex-shrink: 0;
	}

	.target-title {
		flex: 1;
	}

	.cascade-warning {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
	}

	.cascade-warning :global(svg) {
		flex-shrink: 0;
	}

	.affected-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.affected-heading {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0;
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.affected-list {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.affected-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.affected-item :global(svg) {
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.scenario-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.affected-summary {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	.no-dependents {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.modal-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding-top: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.btn-ghost {
		background: transparent;
		color: var(--color-text-secondary);
		border: 1px solid var(--color-border);
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
	}

	.btn-danger {
		background: var(--color-error);
		color: white;
	}

	.btn-danger:hover:not(:disabled) {
		opacity: 0.9;
	}
</style>
