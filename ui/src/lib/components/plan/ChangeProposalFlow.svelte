<script lang="ts">
	/**
	 * ChangeProposalFlow - Manages the ChangeProposal lifecycle UI.
	 *
	 * Displays existing proposals with status transitions and provides
	 * a form to propose new changes against selected Requirements.
	 * Lifecycle: proposed → under_review → accepted | rejected
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import { api } from '$lib/api/client';
	import type { ChangeProposal, ChangeProposalStatus } from '$lib/types/change-proposal';
	import { getChangeProposalStatusInfo } from '$lib/types/change-proposal';
	import type { Requirement } from '$lib/types/requirement';

	interface Props {
		slug: string;
		requirements: Requirement[];
	}

	let { slug, requirements }: Props = $props();

	// Proposals state
	let proposals = $state<ChangeProposal[]>([]);
	let loading = $state(false);
	let error = $state<string | null>(null);

	// Form state
	let showForm = $state(false);
	let formTitle = $state('');
	let formRationale = $state('');
	let formSelectedReqIds = $state<Set<string>>(new Set());
	let submitting = $state(false);
	let formError = $state<string | null>(null);

	// Action loading states
	let actionLoading = $state<Record<string, boolean>>({});

	const activeRequirements = $derived(requirements.filter((r) => r.status === 'active'));

	const openProposals = $derived(
		proposals.filter((p) => p.status === 'proposed' || p.status === 'under_review')
	);
	const closedProposals = $derived(
		proposals.filter(
			(p) => p.status === 'accepted' || p.status === 'rejected' || p.status === 'archived'
		)
	);

	// Load proposals when slug changes
	$effect(() => {
		loadProposals(slug);
	});

	async function loadProposals(planSlug: string): Promise<void> {
		loading = true;
		error = null;
		try {
			proposals = await api.changeProposals.list(planSlug);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load change proposals';
		} finally {
			loading = false;
		}
	}

	function toggleReqSelection(reqId: string): void {
		const next = new Set(formSelectedReqIds);
		if (next.has(reqId)) {
			next.delete(reqId);
		} else {
			next.add(reqId);
		}
		formSelectedReqIds = next;
	}

	async function handleSubmitProposal(): Promise<void> {
		if (!formTitle.trim() || !formRationale.trim() || formSelectedReqIds.size === 0) return;
		submitting = true;
		formError = null;
		try {
			const created = await api.changeProposals.create(slug, {
				title: formTitle.trim(),
				rationale: formRationale.trim(),
				affected_requirement_ids: Array.from(formSelectedReqIds)
			});
			proposals = [created, ...proposals];
			// Reset form
			formTitle = '';
			formRationale = '';
			formSelectedReqIds = new Set();
			showForm = false;
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Failed to create proposal';
		} finally {
			submitting = false;
		}
	}

	async function handleSubmitForReview(proposalId: string): Promise<void> {
		actionLoading = { ...actionLoading, [proposalId]: true };
		try {
			const updated = await api.changeProposals.submit(slug, proposalId);
			proposals = proposals.map((p) => (p.id === proposalId ? updated : p));
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to submit for review';
		} finally {
			const next = { ...actionLoading };
			delete next[proposalId];
			actionLoading = next;
		}
	}

	async function handleAccept(proposalId: string): Promise<void> {
		if (!confirm('Accept this proposal? This will cascade dirty status to affected tasks.')) return;
		actionLoading = { ...actionLoading, [proposalId]: true };
		try {
			const updated = await api.changeProposals.accept(slug, proposalId);
			proposals = proposals.map((p) => (p.id === proposalId ? updated : p));
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to accept proposal';
		} finally {
			const next = { ...actionLoading };
			delete next[proposalId];
			actionLoading = next;
		}
	}

	async function handleReject(proposalId: string): Promise<void> {
		if (!confirm('Reject this proposal? This cannot be undone.')) return;
		actionLoading = { ...actionLoading, [proposalId]: true };
		try {
			const updated = await api.changeProposals.reject(slug, proposalId);
			proposals = proposals.map((p) => (p.id === proposalId ? updated : p));
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to reject proposal';
		} finally {
			const next = { ...actionLoading };
			delete next[proposalId];
			actionLoading = next;
		}
	}

	function statusBadgeClass(status: ChangeProposalStatus): string {
		const info = getChangeProposalStatusInfo(status);
		switch (info.color) {
			case 'blue':
				return 'badge-info';
			case 'orange':
				return 'badge-warning';
			case 'green':
				return 'badge-success';
			case 'red':
				return 'badge-error';
			default:
				return 'badge-neutral';
		}
	}

	function requirementTitle(reqId: string): string {
		return requirements.find((r) => r.id === reqId)?.title ?? reqId;
	}

	function formatDate(iso: string | undefined): string {
		if (!iso) return '';
		return new Date(iso).toLocaleDateString();
	}
</script>

<div class="proposal-flow">
	<div class="panel-header">
		<div class="header-title">
			<Icon name="git-pull-request" size={16} />
			<h3 class="panel-heading">Change Proposals</h3>
			{#if openProposals.length > 0}
				<span class="open-badge">{openProposals.length} open</span>
			{/if}
		</div>
		<button
			class="btn btn-ghost btn-sm"
			onclick={() => (showForm = !showForm)}
			aria-expanded={showForm}
		>
			<Icon name={showForm ? 'x' : 'plus'} size={14} />
			{showForm ? 'Cancel' : 'Propose Change'}
		</button>
	</div>

	{#if error}
		<div class="error-banner" role="alert">
			<Icon name="alert-circle" size={14} />
			<span>{error}</span>
		</div>
	{/if}

	<!-- New Proposal Form -->
	{#if showForm}
		<div class="proposal-form">
			<div class="form-group">
				<label class="form-label" for="prop-title">Title</label>
				<input
					id="prop-title"
					class="form-input"
					type="text"
					bind:value={formTitle}
					placeholder="e.g. Add rate limiting to auth endpoint"
					disabled={submitting}
				/>
			</div>

			<div class="form-group">
				<label class="form-label" for="prop-rationale">Rationale</label>
				<textarea
					id="prop-rationale"
					class="form-textarea"
					bind:value={formRationale}
					placeholder="Why is this change needed? What impact will it have?"
					rows="3"
					disabled={submitting}
				></textarea>
			</div>

			<div class="form-group">
				<span class="form-label">Affected Requirements</span>
				{#if activeRequirements.length === 0}
					<p class="no-reqs-hint">No active requirements to affect.</p>
				{:else}
					<div class="req-checkboxes">
						{#each activeRequirements as req (req.id)}
							<label class="req-checkbox-label" class:checked={formSelectedReqIds.has(req.id)}>
								<input
									type="checkbox"
									checked={formSelectedReqIds.has(req.id)}
									onchange={() => toggleReqSelection(req.id)}
									disabled={submitting}
								/>
								<span>{req.title}</span>
							</label>
						{/each}
					</div>
				{/if}
			</div>

			{#if formError}
				<p class="form-error" role="alert">{formError}</p>
			{/if}

			<div class="form-actions">
				<button
					class="btn btn-primary btn-sm"
					onclick={handleSubmitProposal}
					disabled={submitting || !formTitle.trim() || !formRationale.trim() || formSelectedReqIds.size === 0}
				>
					{submitting ? 'Proposing...' : 'Submit Proposal'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Proposals list -->
	{#if loading}
		<div class="loading-state">
			<Icon name="loader" size={16} />
			<span>Loading proposals...</span>
		</div>
	{:else if proposals.length === 0 && !showForm}
		<div class="empty-state">
			<Icon name="git-pull-request" size={20} />
			<p>No change proposals yet.</p>
		</div>
	{:else}
		<!-- Open proposals -->
		{#if openProposals.length > 0}
			<section class="proposals-section">
				<h4 class="section-heading">Open</h4>
				<ul class="proposals-list" role="list">
					{#each openProposals as proposal (proposal.id)}
						{@const statusInfo = getChangeProposalStatusInfo(proposal.status)}
						{@const isLoading = actionLoading[proposal.id]}

						<li class="proposal-item">
							<div class="proposal-header">
								<span class="proposal-title">{proposal.title}</span>
								<span class="status-badge {statusBadgeClass(proposal.status)}">
									{statusInfo.label}
								</span>
							</div>

							<p class="proposal-rationale">{proposal.rationale}</p>

							{#if proposal.affected_requirement_ids.length > 0}
								<div class="affected-reqs">
									<span class="affected-label">Affects:</span>
									{#each proposal.affected_requirement_ids as reqId}
										<span class="req-chip">{requirementTitle(reqId)}</span>
									{/each}
								</div>
							{/if}

							<div class="proposal-meta">
								<span>Proposed {formatDate(proposal.created_at)}</span>
								{#if proposal.proposed_by}
									<span>by {proposal.proposed_by}</span>
								{/if}
							</div>

							<!-- Action buttons based on status -->
							<div class="proposal-actions">
								{#if proposal.status === 'proposed'}
									<button
										class="btn btn-ghost btn-sm"
										onclick={() => handleSubmitForReview(proposal.id)}
										disabled={isLoading}
									>
										{isLoading ? 'Submitting...' : 'Submit for Review'}
									</button>
								{/if}
								{#if proposal.status === 'under_review'}
									<button
										class="btn btn-primary btn-sm"
										onclick={() => handleAccept(proposal.id)}
										disabled={isLoading}
									>
										{isLoading ? 'Accepting...' : 'Accept'}
									</button>
									<button
										class="btn btn-error btn-sm"
										onclick={() => handleReject(proposal.id)}
										disabled={isLoading}
									>
										{isLoading ? 'Rejecting...' : 'Reject'}
									</button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<!-- Closed proposals (collapsed by default) -->
		{#if closedProposals.length > 0}
			<section class="proposals-section closed-section">
				<h4 class="section-heading muted">
					Closed ({closedProposals.length})
				</h4>
				<ul class="proposals-list" role="list">
					{#each closedProposals as proposal (proposal.id)}
						{@const statusInfo = getChangeProposalStatusInfo(proposal.status)}
						<li class="proposal-item closed">
							<div class="proposal-header">
								<span class="proposal-title">{proposal.title}</span>
								<span class="status-badge {statusBadgeClass(proposal.status)}">
									{statusInfo.label}
								</span>
							</div>
							{#if proposal.decided_at}
								<div class="proposal-meta">
									<span>Decided {formatDate(proposal.decided_at)}</span>
								</div>
							{/if}
							{#if proposal.status === 'accepted'}
								<p class="cascade-note">
									<Icon name="alert-circle" size={12} />
									Cascaded dirty status to affected tasks.
								</p>
							{/if}
						</li>
					{/each}
				</ul>
			</section>
		{/if}
	{/if}
</div>

<style>
	.proposal-flow {
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

	.open-badge {
		padding: 2px var(--space-2);
		background: var(--color-warning-muted);
		color: var(--color-warning);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
	}

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

	/* Form */
	.proposal-form {
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

	.no-reqs-hint {
		margin: 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-style: italic;
	}

	.req-checkboxes {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.req-checkbox-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		cursor: pointer;
		border: 1px solid transparent;
	}

	.req-checkbox-label:hover {
		background: var(--color-bg-secondary);
	}

	.req-checkbox-label.checked {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
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

	.loading-state :global(svg) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	/* Sections */
	.proposals-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.section-heading {
		margin: 0;
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.section-heading.muted {
		color: var(--color-text-muted);
	}

	.proposals-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.proposal-item {
		padding: var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.proposal-item.closed {
		opacity: 0.7;
	}

	.proposal-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-2);
	}

	.proposal-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		flex: 1;
		min-width: 0;
	}

	.status-badge {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		flex-shrink: 0;
		white-space: nowrap;
	}

	.badge-info {
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.badge-warning {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.badge-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.badge-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.badge-neutral {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.proposal-rationale {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-relaxed);
	}

	.affected-reqs {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
	}

	.affected-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.req-chip {
		padding: 2px var(--space-2);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	.proposal-meta {
		display: flex;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.proposal-actions {
		display: flex;
		gap: var(--space-2);
	}

	.cascade-note {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		margin: 0;
		font-size: var(--font-size-xs);
		color: var(--color-warning);
	}

	/* Closed section */
	.closed-section {
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-3);
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

	.btn-primary {
		background: var(--color-accent);
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
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

	.btn-error {
		background: var(--color-error);
		color: white;
	}

	.btn-error:hover:not(:disabled) {
		opacity: 0.9;
	}
</style>
