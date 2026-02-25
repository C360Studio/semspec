<script lang="ts">
	/**
	 * PlanDetail - Detail view for a selected plan.
	 *
	 * Shows plan goal, context, scope, and workflow guidance
	 * based on the current plan stage.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import { api } from '$lib/api/client';
	import { plansStore } from '$lib/stores/plans.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Phase } from '$lib/types/phase';

	interface Props {
		plan: PlanWithStatus;
		phases: Phase[];
		onRefresh?: () => Promise<void>;
		onGeneratePhases?: () => Promise<void>;
	}

	let { plan, phases, onRefresh, onGeneratePhases }: Props = $props();

	let isEditing = $state(false);
	let editGoal = $state('');
	let editContext = $state('');
	let saving = $state(false);
	let approving = $state(false);
	let generatingPhases = $state(false);
	let error = $state<string | null>(null);

	// Workflow guidance based on plan stage
	const guidance = $derived.by(() => {
		if (!plan.approved) {
			return {
				message: 'Review the plan details and approve to generate phases.',
				showApprove: true,
				showEdit: true,
				showGeneratePhases: false
			};
		}

		if (phases.length === 0) {
			return {
				message: 'Generating phases from the plan...',
				showApprove: false,
				showEdit: false,
				showGeneratePhases: true,
				isLoading: generatingPhases
			};
		}

		const unapprovedPhases = phases.filter((p) => p.requires_approval && !p.approved);
		if (unapprovedPhases.length > 0) {
			return {
				message: `Review and approve ${unapprovedPhases.length} phase${unapprovedPhases.length > 1 ? 's' : ''} to continue.`,
				showApprove: false,
				showEdit: false,
				showGeneratePhases: false
			};
		}

		return {
			message: 'All phases approved. Select a phase to view its tasks.',
			showApprove: false,
			showEdit: false,
			showGeneratePhases: false
		};
	});

	const canEdit = $derived(
		!['executing', 'complete', 'failed', 'archived'].includes(plan.stage)
	);

	const hasScope = $derived(
		plan.scope &&
			((plan.scope.include?.length ?? 0) > 0 ||
				(plan.scope.exclude?.length ?? 0) > 0 ||
				(plan.scope.do_not_touch?.length ?? 0) > 0)
	);

	function startEdit(): void {
		editGoal = plan.goal || '';
		editContext = plan.context || '';
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
			await api.plans.update(plan.slug, {
				goal: editGoal,
				context: editContext
			});
			await onRefresh?.();
			isEditing = false;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save changes';
		} finally {
			saving = false;
		}
	}

	async function handleApprove(): Promise<void> {
		approving = true;
		error = null;
		try {
			await plansStore.promote(plan.slug);
			await onRefresh?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to approve plan';
		} finally {
			approving = false;
		}
	}

	async function handleGeneratePhases(): Promise<void> {
		generatingPhases = true;
		error = null;
		try {
			await api.phases.generate(plan.slug);
			await onGeneratePhases?.();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to generate phases';
		} finally {
			generatingPhases = false;
		}
	}
</script>

<div class="plan-detail">
	<!-- Header -->
	<header class="detail-header">
		<div class="header-main">
			<h2 class="detail-title">{plan.title || plan.slug}</h2>
			<StatusBadge status={plan.approved ? 'approved' : 'draft'} />
		</div>
		{#if canEdit && !isEditing}
			<button class="btn btn-ghost btn-sm" onclick={startEdit}>
				<Icon name="edit-2" size={14} />
				Edit
			</button>
		{/if}
	</header>

	{#if error}
		<div class="error-message" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
		</div>
	{/if}

	<!-- Content -->
	<div class="detail-content">
		{#if isEditing}
			<!-- Edit Mode -->
			<div class="detail-section">
				<label class="section-label" for="edit-goal">
					<Icon name="target" size={14} />
					Goal
				</label>
				<textarea
					id="edit-goal"
					class="section-textarea"
					bind:value={editGoal}
					placeholder="What should this plan accomplish?"
					rows="3"
				></textarea>
			</div>

			<div class="detail-section">
				<label class="section-label" for="edit-context">
					<Icon name="info" size={14} />
					Context
				</label>
				<textarea
					id="edit-context"
					class="section-textarea"
					bind:value={editContext}
					placeholder="Additional context, constraints, or requirements..."
					rows="5"
				></textarea>
			</div>

			<div class="edit-actions">
				<button class="btn btn-ghost" onclick={cancelEdit} disabled={saving}>
					Cancel
				</button>
				<button class="btn btn-primary" onclick={saveEdit} disabled={saving}>
					{saving ? 'Saving...' : 'Save Changes'}
				</button>
			</div>
		{:else}
			<!-- View Mode -->
			{#if plan.goal}
				<div class="detail-section">
					<dt class="section-label">
						<Icon name="target" size={14} />
						Goal
					</dt>
					<dd class="section-content">{plan.goal}</dd>
				</div>
			{/if}

			{#if plan.context}
				<div class="detail-section">
					<dt class="section-label">
						<Icon name="info" size={14} />
						Context
					</dt>
					<dd class="section-content">{plan.context}</dd>
				</div>
			{/if}

			{#if hasScope}
				<div class="detail-section">
					<dt class="section-label">
						<Icon name="folder" size={14} />
						Scope
					</dt>
					<dd class="scope-content">
						{#if (plan.scope?.include?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label include">Include</span>
								<ul class="scope-list">
									{#each plan.scope?.include ?? [] as path}
										<li>{path}</li>
									{/each}
								</ul>
							</div>
						{/if}
						{#if (plan.scope?.exclude?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label exclude">Exclude</span>
								<ul class="scope-list">
									{#each plan.scope?.exclude ?? [] as path}
										<li>{path}</li>
									{/each}
								</ul>
							</div>
						{/if}
						{#if (plan.scope?.do_not_touch?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label protected">Protected</span>
								<ul class="scope-list">
									{#each plan.scope?.do_not_touch ?? [] as path}
										<li>{path}</li>
									{/each}
								</ul>
							</div>
						{/if}
					</dd>
				</div>
			{/if}

			{#if !plan.goal && !plan.context && !hasScope}
				<div class="empty-state">
					<Icon name="file-text" size={24} />
					<p>No plan details yet</p>
					{#if canEdit}
						<button class="btn btn-primary btn-sm" onclick={startEdit}>
							Add Details
						</button>
					{/if}
				</div>
			{/if}
		{/if}
	</div>

	<!-- Workflow Guidance -->
	{#if !isEditing}
		<div class="detail-guidance">
			<div class="guidance-hint">
				<Icon name="lightbulb" size={14} />
				<span>{guidance.message}</span>
			</div>
			<div class="guidance-actions">
				{#if guidance.showApprove}
					<button
						class="btn btn-primary"
						onclick={handleApprove}
						disabled={approving}
					>
						{#if approving}
							<Icon name="loader" size={14} />
							Approving...
						{:else}
							<Icon name="check" size={14} />
							Approve Plan
						{/if}
					</button>
				{/if}
				{#if guidance.showGeneratePhases}
					<button
						class="btn btn-primary"
						onclick={handleGeneratePhases}
						disabled={generatingPhases}
					>
						{#if generatingPhases}
							<Icon name="loader" size={14} />
							Generating...
						{:else}
							<Icon name="zap" size={14} />
							Generate Phases
						{/if}
					</button>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.plan-detail {
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
	}

	.detail-title {
		margin: 0;
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
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
		box-shadow: 0 0 0 3px var(--color-accent-muted);
	}

	/* Scope styling */
	.scope-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin: 0;
	}

	.scope-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.scope-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		width: fit-content;
	}

	.scope-label.include {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.scope-label.exclude {
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		color: var(--color-warning);
	}

	.scope-label.protected {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.scope-list {
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.scope-list li {
		font-size: var(--font-size-sm);
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
		padding: var(--space-1) 0;
	}

	.scope-list li::before {
		content: '• ';
		color: var(--color-text-muted);
	}

	/* Edit actions */
	.edit-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding-top: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	/* Empty state */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state p {
		margin: 0;
	}

	/* Workflow guidance */
	.detail-guidance {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
	}

	.guidance-hint {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.guidance-hint :global(svg) {
		flex-shrink: 0;
		margin-top: 2px;
		color: var(--color-warning);
	}

	.guidance-actions {
		display: flex;
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
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
		background: var(--color-accent-hover);
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

	/* Loader animation */
	.btn :global(svg.loader),
	.guidance-actions .btn :global([data-icon='loader']) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
