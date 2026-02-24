<script lang="ts">
	import Modal from '$lib/components/shared/Modal.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import CollapsiblePanel from '$lib/components/shared/CollapsiblePanel.svelte';
	import { api } from '$lib/api/client';
	import type { Phase, PhaseAgentConfig } from '$lib/types/phase';

	interface Props {
		open: boolean;
		phase?: Phase; // undefined = create mode
		planSlug: string;
		allPhases?: Phase[]; // For dependency selection
		onClose: () => void;
		onSave: () => Promise<void>;
	}

	let { open, phase, planSlug, allPhases = [], onClose, onSave }: Props = $props();

	// Form state
	let name = $state('');
	let description = $state('');
	let dependsOn = $state<string[]>([]);
	let requiresApproval = $state(true);
	let agentRoles = $state('');
	let agentModel = $state('');
	let maxConcurrent = $state<number | undefined>(undefined);
	let reviewStrategy = $state<'parallel' | 'sequential' | ''>('');
	let saving = $state(false);
	let error = $state<string | null>(null);

	// Derive mode and title
	const isCreate = $derived(!phase);
	const modalTitle = $derived(isCreate ? 'Create Phase' : 'Edit Phase');

	// Available dependency phases (exclude self)
	const availableDependencies = $derived(
		allPhases.filter((p) => !phase || p.id !== phase.id)
	);

	// Track if form has unsaved changes
	const hasChanges = $derived.by(() => {
		if (!open) return false;
		if (isCreate) {
			return (
				name.trim() !== '' ||
				description.trim() !== '' ||
				dependsOn.length > 0 ||
				!requiresApproval ||
				agentRoles.trim() !== '' ||
				agentModel.trim() !== '' ||
				maxConcurrent !== undefined ||
				reviewStrategy !== ''
			);
		}
		return (
			name !== (phase?.name || '') ||
			description !== (phase?.description || '') ||
			JSON.stringify(dependsOn) !== JSON.stringify(phase?.depends_on || []) ||
			requiresApproval !== (phase?.requires_approval ?? true) ||
			agentRoles !== (phase?.agent_config?.roles?.join(', ') || '') ||
			agentModel !== (phase?.agent_config?.model || '') ||
			maxConcurrent !== phase?.agent_config?.max_concurrent ||
			reviewStrategy !== (phase?.agent_config?.review_strategy || '')
		);
	});

	function handleClose() {
		if (hasChanges && !confirm('Discard unsaved changes?')) {
			return;
		}
		onClose();
	}

	// Reset form when modal opens or phase changes
	$effect(() => {
		if (open) {
			if (phase) {
				name = phase.name || '';
				description = phase.description || '';
				dependsOn = phase.depends_on ? [...phase.depends_on] : [];
				requiresApproval = phase.requires_approval ?? true;
				agentRoles = phase.agent_config?.roles?.join(', ') || '';
				agentModel = phase.agent_config?.model || '';
				maxConcurrent = phase.agent_config?.max_concurrent;
				reviewStrategy = (phase.agent_config?.review_strategy || '') as '' | 'parallel' | 'sequential';
			} else {
				name = '';
				description = '';
				dependsOn = [];
				requiresApproval = true;
				agentRoles = '';
				agentModel = '';
				maxConcurrent = undefined;
				reviewStrategy = '';
			}
			error = null;
			// Focus the name field when modal opens
			setTimeout(() => {
				document.getElementById('phase-name')?.focus();
			}, 0);
		}
	});

	function parseRoles(): string[] | undefined {
		if (!agentRoles.trim()) return undefined;
		return agentRoles
			.split(',')
			.map((r) => r.trim())
			.filter((r) => r.length > 0);
	}

	function buildAgentConfig(): PhaseAgentConfig | undefined {
		const roles = parseRoles();
		const model = agentModel.trim() || undefined;
		const mc = maxConcurrent;
		const rs = reviewStrategy || undefined;

		if (!roles && !model && !mc && !rs) return undefined;

		return {
			roles,
			model,
			max_concurrent: mc,
			review_strategy: rs as 'parallel' | 'sequential' | undefined
		};
	}

	function toggleDependency(phaseId: string) {
		// Check for circular dependency
		if (wouldCreateCircularDependency(phaseId)) {
			error = 'Cannot add this dependency - it would create a circular reference';
			return;
		}

		if (dependsOn.includes(phaseId)) {
			dependsOn = dependsOn.filter((id) => id !== phaseId);
		} else {
			dependsOn = [...dependsOn, phaseId];
		}
		error = null;
	}

	function wouldCreateCircularDependency(newDepId: string): boolean {
		// Simple check: if the new dependency depends on current phase, it's circular
		if (!phase) return false;
		const depPhase = allPhases.find((p) => p.id === newDepId);
		if (!depPhase?.depends_on) return false;
		return depPhase.depends_on.includes(phase.id);
	}

	async function handleSubmit() {
		if (!name.trim()) {
			error = 'Name is required';
			return;
		}

		saving = true;
		error = null;

		try {
			const data = {
				name: name.trim(),
				description: description.trim() || undefined,
				depends_on: dependsOn.length > 0 ? dependsOn : undefined,
				requires_approval: requiresApproval,
				agent_config: buildAgentConfig()
			};

			if (isCreate) {
				await api.phases.create(planSlug, data);
			} else if (phase) {
				await api.phases.update(planSlug, phase.id, data);
			}

			await onSave();
			onClose();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save phase';
		} finally {
			saving = false;
		}
	}
</script>

{#if open}
	<Modal title={modalTitle} onClose={handleClose}>
		<form class="phase-form" onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
			{#if error}
				<div class="error-message" role="alert" aria-live="assertive">
					<Icon name="alert-circle" size={14} />
					<span>{error}</span>
				</div>
			{/if}

			<div class="form-group">
				<label for="phase-name" class="form-label required">Name</label>
				<input
					type="text"
					id="phase-name"
					class="form-input"
					bind:value={name}
					placeholder="e.g., Phase 1: Setup"
					disabled={saving}
				/>
			</div>

			<div class="form-group">
				<label for="phase-description" class="form-label">Description</label>
				<textarea
					id="phase-description"
					class="form-textarea"
					bind:value={description}
					placeholder="Purpose and scope of this phase..."
					rows="3"
					disabled={saving}
				></textarea>
			</div>

			{#if availableDependencies.length > 0}
				<div class="form-group">
					<span class="form-label" id="deps-label">Dependencies</span>
					<div class="dependencies-list" role="group" aria-labelledby="deps-label">
						{#each availableDependencies as depPhase}
							<label class="dependency-item">
								<input
									type="checkbox"
									checked={dependsOn.includes(depPhase.id)}
									onchange={() => toggleDependency(depPhase.id)}
									disabled={saving}
								/>
								<span class="dependency-seq">#{depPhase.sequence}</span>
								<span class="dependency-name">{depPhase.name}</span>
							</label>
						{/each}
					</div>
					<span class="form-hint">Phases that must complete before this one can start</span>
				</div>
			{/if}

			<div class="form-group">
				<label class="checkbox-label">
					<input
						type="checkbox"
						bind:checked={requiresApproval}
						disabled={saving}
					/>
					<span>Requires Approval</span>
				</label>
				<span class="form-hint">If checked, human approval is needed before this phase can execute</span>
			</div>

			<CollapsiblePanel id="phase-agent-config" title="Agent Configuration" defaultOpen={false}>
				<div class="agent-config">
					<div class="form-group">
						<label for="agent-roles" class="form-label">Agent Roles</label>
						<input
							type="text"
							id="agent-roles"
							class="form-input"
							bind:value={agentRoles}
							placeholder="developer, reviewer (comma-separated)"
							disabled={saving}
						/>
						<span class="form-hint">Specific agent roles to assign to this phase</span>
					</div>

					<div class="form-group">
						<label for="agent-model" class="form-label">Model Override</label>
						<input
							type="text"
							id="agent-model"
							class="form-input"
							bind:value={agentModel}
							placeholder="e.g., claude-3-opus"
							disabled={saving}
						/>
						<span class="form-hint">Override the default model for this phase</span>
					</div>

					<div class="form-row">
						<div class="form-group">
							<label for="max-concurrent" class="form-label">Max Concurrent</label>
							<input
								type="number"
								id="max-concurrent"
								class="form-input"
								bind:value={maxConcurrent}
								placeholder="Auto"
								min="1"
								max="10"
								disabled={saving}
							/>
						</div>

						<div class="form-group">
							<label for="review-strategy" class="form-label">Review Strategy</label>
							<select
								id="review-strategy"
								class="form-select"
								bind:value={reviewStrategy}
								disabled={saving}
							>
								<option value="">Default</option>
								<option value="parallel">Parallel</option>
								<option value="sequential">Sequential</option>
							</select>
						</div>
					</div>
				</div>
			</CollapsiblePanel>

			<div class="form-actions">
				<button
					type="button"
					class="btn btn-ghost"
					onclick={handleClose}
					disabled={saving}
				>
					Cancel
				</button>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={saving}
					aria-busy={saving}
				>
					{saving ? 'Saving...' : isCreate ? 'Create Phase' : 'Save Changes'}
				</button>
			</div>
		</form>
	</Modal>
{/if}

<style>
	.phase-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
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

	.form-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.form-row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
	}

	.form-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.form-label.required::after {
		content: ' *';
		color: var(--color-error);
	}

	.form-input,
	.form-textarea,
	.form-select {
		width: 100%;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-family: inherit;
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		transition: border-color var(--transition-fast);
	}

	.form-input:focus,
	.form-textarea:focus,
	.form-select:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 3px var(--color-accent-muted);
	}

	.form-input:disabled,
	.form-textarea:disabled,
	.form-select:disabled {
		background: var(--color-bg-tertiary);
		cursor: not-allowed;
	}

	.form-input::placeholder,
	.form-textarea::placeholder {
		color: var(--color-text-muted);
	}

	.form-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.checkbox-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		cursor: pointer;
	}

	.checkbox-label span {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
	}

	.dependencies-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		max-height: 150px;
		overflow-y: auto;
		padding: var(--space-2);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.dependency-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-sm);
		cursor: pointer;
		transition: background var(--transition-fast);
	}

	.dependency-item:hover {
		background: var(--color-bg-tertiary);
	}

	.dependency-item input[type='checkbox'] {
		flex-shrink: 0;
	}

	.dependency-seq {
		flex-shrink: 0;
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.dependency-name {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.agent-config {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
	}

	.form-actions {
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

	/* Mobile responsive */
	@media (max-width: 600px) {
		.form-row {
			grid-template-columns: 1fr;
		}

		.dependencies-list {
			max-height: 120px;
		}

		.form-actions {
			flex-direction: column;
		}

		.form-actions .btn {
			width: 100%;
		}
	}
</style>
