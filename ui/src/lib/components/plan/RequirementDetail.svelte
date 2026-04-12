<script lang="ts">
	/**
	 * RequirementDetail - Detail view for a selected requirement.
	 *
	 * Shows requirement title/description with edit support,
	 * lists linked scenarios with add/delete CRUD,
	 * and provides deprecate action.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import ScenarioDetail from './ScenarioDetail.svelte';
	import { api } from '$lib/api/client';
	import { LOCKED_STAGES } from '$lib/types/plan';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Requirement } from '$lib/types/requirement';
	import { getRequirementStatusInfo } from '$lib/types/requirement';
	import type { Scenario } from '$lib/types/scenario';

	interface Props {
		requirement: Requirement;
		scenarios: Scenario[];
		plan: PlanWithStatus;
		onRefresh?: () => Promise<void>;
		onRefreshScenarios?: () => Promise<void>;
		onDelete?: (reqId: string) => Promise<void>;
	}

	let { requirement, scenarios, plan, onRefresh, onRefreshScenarios, onDelete }: Props = $props();

	let isEditing = $state(false);
	let editTitle = $state('');
	let editDescription = $state('');
	let saving = $state(false);
	let error = $state<string | null>(null);

	// Add scenario form
	let showAddScenario = $state(false);
	let newGiven = $state('');
	let newWhen = $state('');
	let newThen = $state('');
	let addingScenario = $state(false);

	const statusInfo = $derived(getRequirementStatusInfo(requirement.status));
	const canEdit = $derived(
		requirement.status === 'active' && !LOCKED_STAGES.includes(plan.stage)
	);

	function startEdit(): void {
		editTitle = requirement.title;
		editDescription = requirement.description || '';
		error = null;
		isEditing = true;
	}

	function cancelEdit(): void {
		isEditing = false;
		error = null;
	}

	async function saveEdit(): Promise<void> {
		saving = true;
		error = null;
		try {
			await api.requirements.update(plan.slug, requirement.id, {
				title: editTitle.trim(),
				description: editDescription.trim() || undefined
			});
			await onRefresh?.();
			isEditing = false;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save changes';
		} finally {
			saving = false;
		}
	}

	async function handleDeprecate(): Promise<void> {
		error = null;
		try {
			await api.requirements.deprecate(plan.slug, requirement.id);
			await onRefresh?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to deprecate';
		}
	}

	async function handleDeleteReq(): Promise<void> {
		error = null;
		try {
			await onDelete?.(requirement.id);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to delete';
		}
	}

	async function handleAddScenario(): Promise<void> {
		if (!newGiven.trim() || !newWhen.trim() || !newThen.trim()) return;
		addingScenario = true;
		error = null;
		try {
			await api.scenarios.create(plan.slug, {
				requirement_id: requirement.id,
				given: newGiven.trim(),
				when: newWhen.trim(),
				then: newThen.trim().split('\n').filter(Boolean)
			});
			newGiven = '';
			newWhen = '';
			newThen = '';
			showAddScenario = false;
			await onRefreshScenarios?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to add scenario';
		} finally {
			addingScenario = false;
		}
	}

	async function handleDeleteScenario(scenarioId: string): Promise<void> {
		error = null;
		try {
			await api.scenarios.delete(plan.slug, scenarioId);
			await onRefreshScenarios?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to delete scenario';
		}
	}
</script>

<div class="requirement-detail">
	<header class="detail-header">
		<div class="header-main">
			<h2 class="detail-title">
				{#if isEditing}
					<input
						class="title-input"
						bind:value={editTitle}
						placeholder="Requirement title"
						disabled={saving}
					/>
				{:else}
					{requirement.title}
				{/if}
			</h2>
			<span class="status-badge" data-color={statusInfo.color}>
				<Icon name={statusInfo.icon} size={12} />
				{statusInfo.label}
			</span>
		</div>
		{#if canEdit && !isEditing}
			<div class="header-actions">
				<button class="btn btn-ghost btn-sm" onclick={startEdit}>
					<Icon name="edit-2" size={14} />
					Edit
				</button>
				<button class="btn btn-ghost btn-sm" onclick={handleDeprecate} title="Deprecate">
					<Icon name="archive" size={14} />
				</button>
				<button class="btn btn-ghost btn-sm btn-danger" onclick={handleDeleteReq} title="Delete">
					<Icon name="trash-2" size={14} />
				</button>
			</div>
		{/if}
	</header>

	{#if error}
		<div class="error-message" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
		</div>
	{/if}

	<!-- Description -->
	<div class="detail-content">
		{#if isEditing}
			<div class="detail-section">
				<label class="section-label" for="edit-desc">Description</label>
				<textarea
					id="edit-desc"
					class="section-textarea"
					bind:value={editDescription}
					placeholder="Additional context or acceptance notes..."
					rows="4"
					disabled={saving}
				></textarea>
			</div>
			<div class="edit-actions">
				<button class="btn btn-ghost" onclick={cancelEdit} disabled={saving}>Cancel</button>
				<button class="btn btn-primary" onclick={saveEdit} disabled={saving || !editTitle.trim()}>
					{saving ? 'Saving...' : 'Save'}
				</button>
			</div>
		{:else if requirement.description}
			<div class="detail-section">
				<dt class="section-label">
					<Icon name="align-left" size={14} />
					Description
				</dt>
				<dd class="section-content">{requirement.description}</dd>
			</div>
		{/if}
	</div>

	<!-- Scenarios Section -->
	<div class="scenarios-section">
		<div class="scenarios-header">
			<h3 class="scenarios-title">
				<Icon name="list" size={14} />
				Scenarios
				{#if scenarios.length > 0}
					<span class="count-badge">{scenarios.length}</span>
				{/if}
			</h3>
			{#if canEdit}
				<button
					class="btn btn-ghost btn-sm"
					onclick={() => (showAddScenario = !showAddScenario)}
				>
					<Icon name={showAddScenario ? 'x' : 'plus'} size={14} />
					{showAddScenario ? 'Cancel' : 'Add'}
				</button>
			{/if}
		</div>

		{#if showAddScenario}
			<div class="add-scenario-form">
				<div class="form-group">
					<label class="form-label" for="sc-given">Given</label>
					<input
						id="sc-given"
						class="form-input"
						type="text"
						bind:value={newGiven}
						placeholder="an initial context..."
						disabled={addingScenario}
					/>
				</div>
				<div class="form-group">
					<label class="form-label" for="sc-when">When</label>
					<input
						id="sc-when"
						class="form-input"
						type="text"
						bind:value={newWhen}
						placeholder="an event occurs..."
						disabled={addingScenario}
					/>
				</div>
				<div class="form-group">
					<label class="form-label" for="sc-then">Then (one per line)</label>
					<textarea
						id="sc-then"
						class="form-textarea"
						bind:value={newThen}
						placeholder="expected outcome 1&#10;expected outcome 2"
						rows="3"
						disabled={addingScenario}
					></textarea>
				</div>
				<div class="form-actions">
					<button
						class="btn btn-primary btn-sm"
						onclick={handleAddScenario}
						disabled={addingScenario || !newGiven.trim() || !newWhen.trim() || !newThen.trim()}
					>
						{addingScenario ? 'Adding...' : 'Add Scenario'}
					</button>
				</div>
			</div>
		{/if}

		{#if scenarios.length === 0 && !showAddScenario}
			<div class="empty-scenarios">
				<Icon name="circle" size={16} />
				<p>No scenarios yet.</p>
			</div>
		{:else}
			<div class="scenarios-list">
				{#each scenarios as scenario (scenario.id)}
					<div class="scenario-item">
						<ScenarioDetail {scenario} />
						{#if canEdit}
							<button
								class="delete-scenario-btn"
								onclick={() => handleDeleteScenario(scenario.id)}
								title="Delete scenario"
							>
								<Icon name="trash-2" size={12} />
							</button>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<style>
	.requirement-detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-4);
	}

	.detail-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.header-main {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
		flex: 1;
		min-width: 0;
	}

	.detail-title {
		margin: 0;
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.title-input {
		width: 100%;
		padding: var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		font-family: inherit;
		color: var(--color-text-primary);
		background: var(--color-bg-primary);
	}

	.title-input:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.header-actions {
		display: flex;
		gap: var(--space-1);
		flex-shrink: 0;
	}

	.status-badge {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		font-size: 11px;
		font-weight: var(--font-weight-medium);
		flex-shrink: 0;
	}

	.status-badge[data-color='green'] {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.status-badge[data-color='gray'] {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.status-badge[data-color='orange'] {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.error-message {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	.detail-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.detail-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-accent);
		margin: 0;
	}

	.section-content {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: var(--line-height-relaxed);
		color: var(--color-text-primary);
		white-space: pre-wrap;
	}

	.section-textarea {
		width: 100%;
		padding: var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-family: inherit;
		line-height: var(--line-height-relaxed);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		resize: vertical;
	}

	.section-textarea:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.edit-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding-top: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	/* Scenarios section */
	.scenarios-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.scenarios-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.scenarios-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.count-badge {
		padding: 2px var(--space-2);
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
	}

	.add-scenario-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
	}

	.form-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.form-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.form-input,
	.form-textarea {
		padding: var(--space-2);
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-family: inherit;
		color: var(--color-text-primary);
	}

	.form-input:focus,
	.form-textarea:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.form-textarea {
		resize: vertical;
	}

	.form-actions {
		display: flex;
		justify-content: flex-end;
	}

	.scenarios-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.scenario-item {
		position: relative;
	}

	.delete-scenario-btn {
		position: absolute;
		top: var(--space-2);
		right: var(--space-2);
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
		opacity: 0;
		transition: all var(--transition-fast);
	}

	.scenario-item:hover .delete-scenario-btn {
		opacity: 1;
	}

	.delete-scenario-btn:hover {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		color: var(--color-error);
	}

	.empty-scenarios {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.empty-scenarios p {
		margin: 0;
	}

	/* Buttons */
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
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

	.btn-sm {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
		background: var(--color-accent-hover, var(--color-accent));
		opacity: 0.9;
	}

	.btn-ghost {
		background: transparent;
		color: var(--color-text-secondary);
		border: 1px solid var(--color-border);
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-danger:hover:not(:disabled) {
		border-color: var(--color-error);
		color: var(--color-error);
	}
</style>
