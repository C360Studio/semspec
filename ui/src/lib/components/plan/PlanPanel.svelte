<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import ChatDrawerTrigger from '$lib/components/chat/ChatDrawerTrigger.svelte';
	import { api } from '$lib/api/client';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plan: PlanWithStatus;
		onRefresh?: () => Promise<void>;
	}

	let { plan, onRefresh }: Props = $props();

	// Edit mode state
	let isEditing = $state(false);
	let editGoal = $state('');
	let editContext = $state('');
	let saving = $state(false);
	let error = $state<string | null>(null);

	// Can edit if plan is not yet in executing/complete/failed states
	const canEdit = $derived(
		!['implementing', 'executing', 'complete', 'failed', 'archived'].includes(plan.stage)
	);

	const hasScope = $derived(
		plan.scope &&
			((plan.scope.include?.length ?? 0) > 0 ||
				(plan.scope.exclude?.length ?? 0) > 0 ||
				(plan.scope.do_not_touch?.length ?? 0) > 0)
	);

	function startEdit() {
		editGoal = plan.goal || '';
		editContext = plan.context || '';
		error = null;
		isEditing = true;
	}

	function cancelEdit() {
		isEditing = false;
		error = null;
	}

	async function saveEdit() {
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
</script>

<div class="plan-panel">
	<div class="panel-header">
		<h3 class="panel-title">Plan Details</h3>
		{#if canEdit && !isEditing}
			<button class="edit-btn" onclick={startEdit}>
				<Icon name="edit-2" size={14} />
				<span>Edit</span>
			</button>
		{/if}
	</div>

	{#if error}
		<div class="error-message" role="alert" aria-live="assertive">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
		</div>
	{/if}

	<div class="plan-sections">
		{#if isEditing}
			<!-- Edit Mode -->
			<div class="plan-section">
				<div class="section-header">
					<Icon name="target" size={14} />
					<span class="section-label">Goal</span>
				</div>
				<textarea
					class="section-textarea"
					bind:value={editGoal}
					placeholder="What should this plan accomplish?"
					rows="3"
				></textarea>
			</div>

			<div class="plan-section">
				<div class="section-header">
					<Icon name="info" size={14} />
					<span class="section-label">Context</span>
				</div>
				<textarea
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
				<button
					class="btn btn-primary"
					onclick={saveEdit}
					disabled={saving}
					aria-busy={saving}
				>
					{saving ? 'Saving...' : 'Save Changes'}
				</button>
			</div>
		{:else}
			<!-- View Mode -->
			{#if plan.goal}
				<div class="plan-section">
					<div class="section-header">
						<Icon name="target" size={14} />
						<span class="section-label">Goal</span>
						<div class="section-actions">
							<ChatDrawerTrigger
								context={{ type: 'plan', planSlug: plan.slug }}
								variant="icon"
								class="section-chat-trigger"
							/>
						</div>
					</div>
					<p class="section-content">{plan.goal}</p>
				</div>
			{/if}

			{#if plan.context}
				<div class="plan-section">
					<div class="section-header">
						<Icon name="info" size={14} />
						<span class="section-label">Context</span>
						<div class="section-actions">
							<ChatDrawerTrigger
								context={{ type: 'plan', planSlug: plan.slug }}
								variant="icon"
								class="section-chat-trigger"
							/>
						</div>
					</div>
					<p class="section-content">{plan.context}</p>
				</div>
			{/if}

			{#if hasScope}
				<div class="plan-section">
					<div class="section-header">
						<Icon name="folder" size={14} />
						<span class="section-label">Scope</span>
					</div>
					<div class="scope-content">
						{#if (plan.scope?.include?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label include">Include</span>
								<ul class="scope-list">
									{#each plan.scope?.include ?? [] as path}
										<li class="scope-item">{path}</li>
									{/each}
								</ul>
							</div>
						{/if}

						{#if (plan.scope?.exclude?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label exclude">Exclude</span>
								<ul class="scope-list">
									{#each plan.scope?.exclude ?? [] as path}
										<li class="scope-item">{path}</li>
								{/each}
								</ul>
							</div>
						{/if}

						{#if (plan.scope?.do_not_touch?.length ?? 0) > 0}
							<div class="scope-group">
								<span class="scope-label protected">Protected</span>
								<ul class="scope-list">
									{#each plan.scope?.do_not_touch ?? [] as path}
										<li class="scope-item">{path}</li>
									{/each}
								</ul>
							</div>
						{/if}
					</div>
				</div>
			{/if}

			{#if !plan.goal && !plan.context && !hasScope}
				<div class="empty-state">
					<Icon name="file-text" size={24} />
					<p>No plan details yet</p>
					{#if canEdit}
						<button class="btn btn-primary" onclick={startEdit}>
							Add Details
						</button>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</div>

<style>
	.plan-panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.panel-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.edit-btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.edit-btn:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.edit-btn:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.error-message {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted, rgba(239, 68, 68, 0.1));
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		color: var(--color-error);
	}

	.plan-sections {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.plan-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-accent);
	}

	.section-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.section-actions {
		margin-left: auto;
	}

	.section-actions :global(.section-chat-trigger) {
		width: 24px;
		height: 24px;
		border: none;
		background: transparent;
	}

	.section-actions :global(.section-chat-trigger:hover) {
		background: var(--color-bg-tertiary);
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
		min-height: 80px;
		transition: border-color var(--transition-fast);
	}

	.section-textarea:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 3px var(--color-accent-muted);
	}

	.section-textarea::placeholder {
		color: var(--color-text-muted);
	}

	.edit-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding-top: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

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

	.btn:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
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
	}

	.btn-ghost:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.section-content {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		line-height: var(--line-height-relaxed);
	}

	.scope-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
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

	.scope-item {
		font-size: var(--font-size-sm);
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
		padding: var(--space-1) 0;
	}

	.scope-item::before {
		content: '• ';
		color: var(--color-text-muted);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6) 0;
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
	}
</style>
