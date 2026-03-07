<script lang="ts">
	/**
	 * RequirementPanel - Lists plan-scoped requirements with linked scenarios.
	 *
	 * Fetches requirements and their scenarios from the API on mount.
	 * Supports expanding a requirement to view linked scenarios inline
	 * and provides an "Add Requirement" form.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import ScenarioDetail from './ScenarioDetail.svelte';
	import { api } from '$lib/api/client';
	import type { Requirement, RequirementStatus } from '$lib/types/requirement';
	import { getRequirementStatusInfo } from '$lib/types/requirement';
	import type { Scenario } from '$lib/types/scenario';

	interface Props {
		slug: string;
	}

	let { slug }: Props = $props();

	// State
	let requirements = $state<Requirement[]>([]);
	let scenariosByReq = $state<Record<string, Scenario[]>>({});
	let expandedIds = $state<Set<string>>(new Set());
	let loadingReqs = $state(false);
	let loadingScenarios = $state<Set<string>>(new Set());
	let error = $state<string | null>(null);

	// Add form state
	let showAddForm = $state(false);
	let newTitle = $state('');
	let newDescription = $state('');
	let submitting = $state(false);
	let submitError = $state<string | null>(null);

	// Computed counts
	const activeCount = $derived(requirements.filter((r) => r.status === 'active').length);

	// Load requirements when slug changes
	$effect(() => {
		loadRequirements(slug);
	});

	async function loadRequirements(planSlug: string): Promise<void> {
		loadingReqs = true;
		error = null;
		try {
			requirements = await api.requirements.list(planSlug);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load requirements';
		} finally {
			loadingReqs = false;
		}
	}

	async function toggleExpand(reqId: string): Promise<void> {
		const next = new Set(expandedIds);
		if (next.has(reqId)) {
			next.delete(reqId);
			expandedIds = next;
			return;
		}
		next.add(reqId);
		expandedIds = next;

		// Fetch scenarios if not already loaded
		if (!scenariosByReq[reqId]) {
			const loading = new Set(loadingScenarios);
			loading.add(reqId);
			loadingScenarios = loading;
			try {
				const scenarios = await api.scenarios.listByRequirement(slug, reqId);
				scenariosByReq = { ...scenariosByReq, [reqId]: scenarios };
			} catch {
				scenariosByReq = { ...scenariosByReq, [reqId]: [] };
			} finally {
				const loading2 = new Set(loadingScenarios);
				loading2.delete(reqId);
				loadingScenarios = loading2;
			}
		}
	}

	async function handleDeprecate(reqId: string): Promise<void> {
		try {
			const updated = await api.requirements.deprecate(slug, reqId);
			requirements = requirements.map((r) => (r.id === reqId ? updated : r));
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to deprecate requirement';
		}
	}

	async function handleAddRequirement(): Promise<void> {
		if (!newTitle.trim()) return;
		submitting = true;
		submitError = null;
		try {
			const created = await api.requirements.create(slug, {
				title: newTitle.trim(),
				description: newDescription.trim() || undefined
			});
			requirements = [...requirements, created];
			newTitle = '';
			newDescription = '';
			showAddForm = false;
		} catch (err) {
			submitError = err instanceof Error ? err.message : 'Failed to create requirement';
		} finally {
			submitting = false;
		}
	}

	function statusBadgeClass(status: RequirementStatus): string {
		const info = getRequirementStatusInfo(status);
		switch (info.color) {
			case 'green':
				return 'badge-success';
			case 'orange':
				return 'badge-warning';
			default:
				return 'badge-neutral';
		}
	}
</script>

<div class="requirement-panel">
	<div class="panel-header">
		<div class="header-title">
			<Icon name="list-checks" size={16} />
			<h3 class="panel-heading">Requirements</h3>
			{#if activeCount > 0}
				<span class="count-badge">{activeCount} active</span>
			{/if}
		</div>
		<button
			type="button"
			class="btn btn-ghost btn-sm"
			onclick={() => (showAddForm = !showAddForm)}
			aria-expanded={showAddForm}
		>
			<Icon name={showAddForm ? 'x' : 'plus'} size={14} />
			{showAddForm ? 'Cancel' : 'Add'}
		</button>
	</div>

	{#if error}
		<div class="error-banner" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
			<button class="btn-link" onclick={() => loadRequirements(slug)}>Retry</button>
		</div>
	{/if}

	<!-- Add Requirement Form -->
	{#if showAddForm}
		<div class="add-form">
			<div class="form-group">
				<label class="form-label" for="req-title">Title</label>
				<input
					id="req-title"
					class="form-input"
					type="text"
					bind:value={newTitle}
					placeholder="e.g. User can reset their password"
					disabled={submitting}
				/>
			</div>
			<div class="form-group">
				<label class="form-label" for="req-description">Description (optional)</label>
				<textarea
					id="req-description"
					class="form-textarea"
					bind:value={newDescription}
					placeholder="Additional context or acceptance notes..."
					rows="2"
					disabled={submitting}
				></textarea>
			</div>
			{#if submitError}
				<p class="form-error" role="alert">{submitError}</p>
			{/if}
			<div class="form-actions">
				<button
					class="btn btn-primary btn-sm"
					onclick={handleAddRequirement}
					disabled={submitting || !newTitle.trim()}
				>
					{submitting ? 'Adding...' : 'Add Requirement'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Requirements List -->
	{#if loadingReqs}
		<div class="loading-state">
			<Icon name="loader" size={16} />
			<span>Loading requirements...</span>
		</div>
	{:else if requirements.length === 0 && !showAddForm}
		<div class="empty-state">
			<Icon name="circle" size={20} />
			<p>No requirements yet.</p>
			<button class="btn-link" onclick={() => (showAddForm = true)}>Add the first requirement</button>
		</div>
	{:else}
		<ul class="requirement-list" role="list">
			{#each requirements as req (req.id)}
				{@const expanded = expandedIds.has(req.id)}
				{@const scenarios = scenariosByReq[req.id] ?? []}
				{@const isLoadingScenarios = loadingScenarios.has(req.id)}
				{@const statusInfo = getRequirementStatusInfo(req.status)}

				<li class="requirement-item" data-status={req.status}>
					<div class="req-header">
						<button
							type="button"
							class="expand-btn"
							onclick={() => toggleExpand(req.id)}
							aria-expanded={expanded}
							aria-label={expanded ? 'Collapse scenarios' : 'Expand scenarios'}
						>
							<Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={14} />
						</button>

						<div class="req-main">
							<span class="req-title">{req.title}</span>
							<span class="req-status-badge {statusBadgeClass(req.status)}">
								{statusInfo.label}
							</span>
						</div>

						{#if req.status === 'active'}
							<button
								type="button"
								class="btn btn-ghost btn-xs deprecate-btn"
								onclick={() => handleDeprecate(req.id)}
								title="Deprecate this requirement"
							>
								<Icon name="archive" size={12} />
							</button>
						{/if}
					</div>

					{#if req.description}
						<p class="req-description">{req.description}</p>
					{/if}

					<!-- Linked Scenarios -->
					{#if expanded}
						<div class="scenarios-container">
							{#if isLoadingScenarios}
								<div class="loading-inline">
									<Icon name="loader" size={12} />
									<span>Loading scenarios...</span>
								</div>
							{:else if scenarios.length === 0}
								<p class="no-scenarios">No scenarios linked to this requirement.</p>
							{:else}
								<div class="scenarios-list">
									{#each scenarios as scenario (scenario.id)}
										<ScenarioDetail {scenario} />
									{/each}
								</div>
							{/if}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.requirement-panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.header-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.panel-heading {
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

	/* Error banner */
	.error-banner {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	/* Add form */
	.add-form {
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

	.form-error {
		margin: 0;
		font-size: var(--font-size-xs);
		color: var(--color-error);
	}

	.form-actions {
		display: flex;
		justify-content: flex-end;
	}

	/* Loading / empty */
	.loading-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		text-align: center;
	}

	.loading-state {
		flex-direction: row;
		justify-content: center;
	}

	/* Loading animation */
	.loading-state :global(svg),
	.loading-inline :global(svg) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	/* Requirements list */
	.requirement-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.requirement-item {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.requirement-item[data-status='deprecated'] {
		opacity: 0.6;
	}

	.requirement-item[data-status='superseded'] {
		border-style: dashed;
	}

	.req-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
	}

	.expand-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		padding: 0;
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
		flex-shrink: 0;
	}

	.expand-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.req-main {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1;
		min-width: 0;
	}

	.req-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		flex: 1;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.req-status-badge {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		flex-shrink: 0;
	}

	.badge-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.badge-warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.badge-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.deprecate-btn {
		opacity: 0;
		flex-shrink: 0;
	}

	.req-header:hover .deprecate-btn {
		opacity: 1;
	}

	.req-description {
		margin: 0;
		padding: var(--space-2) var(--space-3) var(--space-2) calc(20px + var(--space-2) + var(--space-3));
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-relaxed);
		border-top: 1px solid var(--color-border);
		background: var(--color-bg-primary);
	}

	/* Scenarios */
	.scenarios-container {
		padding: var(--space-3);
		border-top: 1px solid var(--color-border);
		background: var(--color-bg-primary);
	}

	.loading-inline {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.no-scenarios {
		margin: 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-style: italic;
	}

	.scenarios-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-xs);
	}

	.btn-xs {
		padding: 2px var(--space-2);
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

	.btn-link {
		background: none;
		border: none;
		padding: 0;
		font-size: inherit;
		color: var(--color-accent);
		cursor: pointer;
		text-decoration: underline;
	}

	.btn-link:hover {
		opacity: 0.8;
	}
</style>
