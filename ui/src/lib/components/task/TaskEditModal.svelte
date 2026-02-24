<script lang="ts">
	import Modal from '$lib/components/shared/Modal.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import AcceptanceCriteriaEditor from './AcceptanceCriteriaEditor.svelte';
	import { api } from '$lib/api/client';
	import type { Task, TaskType, AcceptanceCriterion } from '$lib/types/task';

	interface Props {
		open: boolean;
		task?: Task; // undefined = create mode
		planSlug: string;
		allTasks?: Task[]; // For dependency selection
		phaseId?: string; // Pre-select phase when creating task from phase
		onClose: () => void;
		onSave: () => Promise<void>;
	}

	let { open, task, planSlug, allTasks = [], phaseId, onClose, onSave }: Props = $props();

	// Form state
	let description = $state('');
	let type = $state<TaskType | ''>('');
	let acceptanceCriteria = $state<AcceptanceCriterion[]>([]);
	let filesText = $state('');
	let dependsOn = $state<string[]>([]);
	let saving = $state(false);
	let error = $state<string | null>(null);

	// Derive mode and title
	const isCreate = $derived(!task);
	const modalTitle = $derived(isCreate ? 'Create Task' : 'Edit Task');

	// Available dependency tasks (exclude self)
	const availableDependencies = $derived(
		allTasks.filter((t) => !task || t.id !== task.id)
	);

	// Track if form has unsaved changes
	const hasChanges = $derived.by(() => {
		if (!open) return false;
		if (isCreate) {
			return (
				description.trim() !== '' ||
				type !== '' ||
				acceptanceCriteria.length > 0 ||
				filesText.trim() !== '' ||
				dependsOn.length > 0
			);
		}
		return (
			description !== (task?.description || '') ||
			type !== (task?.type || '') ||
			JSON.stringify(acceptanceCriteria) !== JSON.stringify(task?.acceptance_criteria || []) ||
			filesText !== (task?.files?.join('\n') || '') ||
			JSON.stringify(dependsOn) !== JSON.stringify(task?.depends_on || [])
		);
	});

	function handleClose() {
		if (hasChanges && !confirm('Discard unsaved changes?')) {
			return;
		}
		onClose();
	}

	// Reset form when modal opens or task changes
	$effect(() => {
		if (open) {
			if (task) {
				description = task.description || '';
				type = task.type || '';
				acceptanceCriteria = task.acceptance_criteria ? [...task.acceptance_criteria] : [];
				filesText = task.files?.join('\n') || '';
				dependsOn = task.depends_on ? [...task.depends_on] : [];
			} else {
				description = '';
				type = '';
				acceptanceCriteria = [];
				filesText = '';
				dependsOn = [];
			}
			error = null;
			// Focus the description field when modal opens
			setTimeout(() => {
				document.getElementById('task-description')?.focus();
			}, 0);
		}
	});

	const taskTypes: { value: TaskType; label: string }[] = [
		{ value: 'implement', label: 'Implementation' },
		{ value: 'test', label: 'Testing' },
		{ value: 'document', label: 'Documentation' },
		{ value: 'review', label: 'Review' },
		{ value: 'refactor', label: 'Refactoring' }
	];

	function parseFiles(): string[] {
		return filesText
			.split(/[\n,]/)
			.map((f) => f.trim())
			.filter((f) => f.length > 0);
	}

	function toggleDependency(taskId: string) {
		if (dependsOn.includes(taskId)) {
			dependsOn = dependsOn.filter((id) => id !== taskId);
		} else {
			dependsOn = [...dependsOn, taskId];
		}
	}

	async function handleSubmit() {
		if (!description.trim()) {
			error = 'Description is required';
			return;
		}

		// Validate acceptance criteria - if any exist, all fields must be filled
		const invalidCriteria = acceptanceCriteria.some(
			(ac) => !ac.given.trim() || !ac.when.trim() || !ac.then.trim()
		);
		if (invalidCriteria) {
			error = 'All acceptance criteria fields (Given/When/Then) must be filled';
			return;
		}

		saving = true;
		error = null;

		try {
			const data = {
				description: description.trim(),
				type: type || undefined,
				acceptance_criteria: acceptanceCriteria.length > 0 ? acceptanceCriteria : undefined,
				files: parseFiles().length > 0 ? parseFiles() : undefined,
				depends_on: dependsOn.length > 0 ? dependsOn : undefined
			};

			if (isCreate) {
				await api.tasks.create(planSlug, data);
			} else if (task) {
				await api.tasks.update(planSlug, task.id, data);
			}

			await onSave();
			onClose();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save task';
		} finally {
			saving = false;
		}
	}
</script>

{#if open}
	<Modal title={modalTitle} onClose={handleClose}>
		<form class="task-form" onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
			{#if error}
				<div class="error-message" role="alert" aria-live="assertive">
					<Icon name="alert-circle" size={14} />
					<span>{error}</span>
				</div>
			{/if}

			<div class="form-group">
				<label for="task-description" class="form-label required">Description</label>
				<textarea
					id="task-description"
					class="form-textarea"
					bind:value={description}
					placeholder="What needs to be done?"
					rows="3"
					disabled={saving}
				></textarea>
			</div>

			<div class="form-group">
				<label for="task-type" class="form-label">Type</label>
				<select
					id="task-type"
					class="form-select"
					bind:value={type}
					disabled={saving}
				>
					<option value="">Select type...</option>
					{#each taskTypes as { value, label }}
						<option {value}>{label}</option>
					{/each}
				</select>
			</div>

			<div class="form-group">
				<span class="form-label" id="ac-label">Acceptance Criteria</span>
				<div role="group" aria-labelledby="ac-label">
					<AcceptanceCriteriaEditor
						criteria={acceptanceCriteria}
						onUpdate={(c) => (acceptanceCriteria = c)}
						disabled={saving}
					/>
				</div>
			</div>

			<div class="form-group">
				<label for="task-files" class="form-label">Files</label>
				<textarea
					id="task-files"
					class="form-textarea mono"
					bind:value={filesText}
					placeholder="One file path per line..."
					rows="3"
					disabled={saving}
				></textarea>
				<span class="form-hint">Enter file paths, one per line or comma-separated</span>
			</div>

			{#if availableDependencies.length > 0}
				<div class="form-group">
					<span class="form-label" id="deps-label">Dependencies</span>
					<div class="dependencies-list" role="group" aria-labelledby="deps-label">
						{#each availableDependencies as depTask}
							<label class="dependency-item">
								<input
									type="checkbox"
									checked={dependsOn.includes(depTask.id)}
									onchange={() => toggleDependency(depTask.id)}
									disabled={saving}
								/>
								<span class="dependency-seq">#{depTask.sequence}</span>
								<span class="dependency-desc">{depTask.description}</span>
							</label>
						{/each}
					</div>
					<span class="form-hint">Tasks that must complete before this one can start</span>
				</div>
			{/if}

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
					{saving ? 'Saving...' : isCreate ? 'Create Task' : 'Save Changes'}
				</button>
			</div>
		</form>
	</Modal>
{/if}

<style>
	.task-form {
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

	.form-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.form-label.required::after {
		content: ' *';
		color: var(--color-error);
	}

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

	.form-textarea.mono {
		font-family: var(--font-family-mono);
	}

	.form-textarea:focus,
	.form-select:focus {
		outline: none;
		border-color: var(--color-accent);
		box-shadow: 0 0 0 3px var(--color-accent-muted);
	}

	.form-textarea:disabled,
	.form-select:disabled {
		background: var(--color-bg-tertiary);
		cursor: not-allowed;
	}

	.form-textarea::placeholder {
		color: var(--color-text-muted);
	}

	.form-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.dependencies-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		max-height: 200px;
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

	.dependency-desc {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
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
		.dependencies-list {
			max-height: 150px;
		}

		.dependency-desc {
			font-size: var(--font-size-xs);
		}

		.form-actions {
			flex-direction: column;
		}

		.form-actions .btn {
			width: 100%;
		}
	}
</style>
