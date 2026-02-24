<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { AcceptanceCriterion } from '$lib/types/task';

	interface Props {
		criteria: AcceptanceCriterion[];
		onUpdate: (criteria: AcceptanceCriterion[]) => void;
		disabled?: boolean;
	}

	let { criteria, onUpdate, disabled = false }: Props = $props();

	function addCriterion() {
		const newIndex = criteria.length;
		onUpdate([...criteria, { given: '', when: '', then: '' }]);
		// Focus first field of new criterion after DOM update
		setTimeout(() => {
			document.getElementById(`ac-${newIndex}-given`)?.focus();
		}, 0);
	}

	function updateCriterion(index: number, field: keyof AcceptanceCriterion, value: string) {
		const updated = [...criteria];
		updated[index] = { ...updated[index], [field]: value };
		onUpdate(updated);
	}

	function removeCriterion(index: number) {
		onUpdate(criteria.filter((_, i) => i !== index));
		// Move focus to previous criterion or add button after removal
		setTimeout(() => {
			if (criteria.length <= 1) {
				// Will be empty after removal, focus add button
				document.querySelector<HTMLButtonElement>('.add-btn')?.focus();
			} else {
				// Focus previous criterion's first field
				const targetIndex = Math.max(0, index - 1);
				document.getElementById(`ac-${targetIndex}-given`)?.focus();
			}
		}, 0);
	}
</script>

<div class="ac-editor">
	{#if criteria.length === 0}
		<div class="empty-state">
			<p>No acceptance criteria defined</p>
			{#if !disabled}
				<button class="add-btn" onclick={addCriterion}>
					<Icon name="plus" size={14} />
					<span>Add Criterion</span>
				</button>
			{/if}
		</div>
	{:else}
		<div class="criteria-list">
			{#each criteria as criterion, index}
				<div class="criterion-item">
					<div class="criterion-header">
						<span class="criterion-number">#{index + 1}</span>
						{#if !disabled}
							<button
								class="remove-btn"
								onclick={() => removeCriterion(index)}
								aria-label="Remove criterion {index + 1}"
							>
								<Icon name="x" size={14} />
							</button>
						{/if}
					</div>
					<div class="criterion-fields">
						<div class="field-row">
							<label class="field-label given" for="ac-{index}-given">Given</label>
							<input
								id="ac-{index}-given"
								type="text"
								class="field-input"
								value={criterion.given}
								oninput={(e) => updateCriterion(index, 'given', e.currentTarget.value)}
								placeholder="Initial condition..."
								{disabled}
							/>
						</div>
						<div class="field-row">
							<label class="field-label when" for="ac-{index}-when">When</label>
							<input
								id="ac-{index}-when"
								type="text"
								class="field-input"
								value={criterion.when}
								oninput={(e) => updateCriterion(index, 'when', e.currentTarget.value)}
								placeholder="Action or event..."
								{disabled}
							/>
						</div>
						<div class="field-row">
							<label class="field-label then" for="ac-{index}-then">Then</label>
							<input
								id="ac-{index}-then"
								type="text"
								class="field-input"
								value={criterion.then}
								oninput={(e) => updateCriterion(index, 'then', e.currentTarget.value)}
								placeholder="Expected outcome..."
								{disabled}
							/>
						</div>
					</div>
				</div>
			{/each}
		</div>
		{#if !disabled}
			<button class="add-btn add-more" onclick={addCriterion}>
				<Icon name="plus" size={14} />
				<span>Add Another</span>
			</button>
		{/if}
	{/if}
</div>

<style>
	.ac-editor {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px dashed var(--color-border);
		border-radius: var(--radius-md);
		text-align: center;
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.criteria-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.criterion-item {
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.criterion-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-2);
	}

	.criterion-number {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
	}

	.remove-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		padding: 0;
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.remove-btn:hover {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
	}

	.remove-btn:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.criterion-fields {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.field-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.field-label {
		flex-shrink: 0;
		width: 50px;
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.field-label.given {
		color: var(--color-accent);
	}

	.field-label.when {
		color: var(--color-warning);
	}

	.field-label.then {
		color: var(--color-success);
	}

	.field-input {
		flex: 1;
		padding: var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		transition: border-color var(--transition-fast);
	}

	.field-input:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 3px var(--color-accent-muted);
	}

	.field-input:disabled {
		background: var(--color-bg-tertiary);
		cursor: not-allowed;
	}

	.field-input::placeholder {
		color: var(--color-text-muted);
	}

	.add-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		background: transparent;
		border: 1px dashed var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.add-btn:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.add-btn:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.add-more {
		align-self: flex-start;
	}
</style>
