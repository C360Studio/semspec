<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import { PULL_INTERVAL_OPTIONS } from '$lib/types/source';
	import type { AddRepositoryRequest } from '$lib/types/source';

	interface Props {
		open: boolean;
		loading?: boolean;
		onclose: () => void;
		onsubmit: (request: AddRepositoryRequest) => void;
	}

	let { open, loading = false, onclose, onsubmit }: Props = $props();

	let url = $state('');
	let branch = $state('');
	let project = $state('');
	let autoPull = $state(false);
	let pullInterval = $state('');
	let urlError = $state<string | null>(null);

	const isValidUrl = $derived(
		Boolean(url) &&
		(url.startsWith('https://') ||
			url.startsWith('git@') ||
			url.startsWith('git://') ||
			url.startsWith('ssh://'))
	);

	function validateUrl() {
		if (!url) {
			urlError = 'Repository URL is required';
		} else if (!isValidUrl) {
			urlError = 'Invalid git URL format';
		} else {
			urlError = null;
		}
	}

	function handleSubmit() {
		validateUrl();
		if (urlError) return;

		onsubmit({
			url: url.trim(),
			branch: branch.trim() || undefined,
			projectId: project.trim() || 'default',
			autoPull,
			pullInterval: autoPull ? pullInterval : undefined
		});
	}

	function handleClose() {
		if (!loading) {
			url = '';
			branch = '';
			project = '';
			autoPull = false;
			pullInterval = '';
			urlError = null;
			onclose();
		}
	}

	function handleUrlInput() {
		if (urlError) {
			validateUrl();
		}
	}
</script>

{#if open}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div class="modal-backdrop" onclick={handleClose} role="presentation">
		<div
			class="modal"
			onclick={(e) => e.stopPropagation()}
			role="dialog"
			aria-modal="true"
			aria-labelledby="add-repo-title"
			tabindex="-1"
		>
			<header class="modal-header">
				<h2 id="add-repo-title">Add Repository</h2>
				<button
					class="close-button"
					onclick={handleClose}
					disabled={loading}
					aria-label="Close"
				>
					<Icon name="x" size={20} />
				</button>
			</header>

			<div class="modal-body">
				<div class="form-group">
					<label for="repo-url">Git URL <span class="required">*</span></label>
					<input
						id="repo-url"
						type="url"
						bind:value={url}
						oninput={handleUrlInput}
						onblur={validateUrl}
						placeholder="https://github.com/owner/repo.git"
						class:error={urlError}
						disabled={loading}
					/>
					{#if urlError}
						<span class="error-message">{urlError}</span>
					{/if}
					<span class="hint">Supports HTTPS, SSH, and git:// protocols</span>
				</div>

				<div class="form-group">
					<label for="repo-branch">Branch (optional)</label>
					<input
						id="repo-branch"
						type="text"
						bind:value={branch}
						placeholder="main"
						disabled={loading}
					/>
					<span class="hint">Leave empty to use default branch</span>
				</div>

				<div class="form-group">
					<label for="repo-project">Project Tag (optional)</label>
					<input
						id="repo-project"
						type="text"
						bind:value={project}
						placeholder="e.g., auth-system"
						disabled={loading}
					/>
					<span class="hint">Group related sources together</span>
				</div>

				<div class="form-group auto-pull-group">
					<label class="checkbox-label">
						<input
							type="checkbox"
							bind:checked={autoPull}
							disabled={loading}
						/>
						<span>Auto-pull updates</span>
					</label>

					{#if autoPull}
						<select
							bind:value={pullInterval}
							class="pull-interval"
							disabled={loading}
						>
							{#each PULL_INTERVAL_OPTIONS as option}
								<option value={option.value}>{option.label}</option>
							{/each}
						</select>
					{/if}
				</div>
			</div>

			<footer class="modal-footer">
				<button class="btn btn-secondary" onclick={handleClose} disabled={loading}>
					Cancel
				</button>
				<button
					class="btn btn-primary"
					onclick={handleSubmit}
					disabled={!url || loading}
				>
					{#if loading}
						<Icon name="loader" size={16} />
						Adding...
					{:else}
						<Icon name="git-branch" size={16} />
						Add Repository
					{/if}
				</button>
			</footer>
		</div>
	</div>
{/if}

<style>
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.5);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 1000;
	}

	.modal {
		background: var(--color-bg-primary);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-lg);
		width: 100%;
		max-width: 500px;
		max-height: 90vh;
		overflow: hidden;
		display: flex;
		flex-direction: column;
	}

	.modal-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.modal-header h2 {
		margin: 0;
		font-size: var(--font-size-lg);
	}

	.close-button {
		padding: var(--space-1);
		background: none;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
	}

	.close-button:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.close-button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.modal-body {
		padding: var(--space-4);
		overflow-y: auto;
	}

	.form-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-bottom: var(--space-4);
	}

	.form-group:last-child {
		margin-bottom: 0;
	}

	.form-group label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.required {
		color: var(--color-error);
	}

	.form-group input[type="text"],
	.form-group input[type="url"] {
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.form-group input:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.form-group input.error {
		border-color: var(--color-error);
	}

	.form-group input:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.error-message {
		font-size: var(--font-size-xs);
		color: var(--color-error);
	}

	.auto-pull-group {
		flex-direction: row;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-3);
	}

	.checkbox-label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		cursor: pointer;
	}

	.checkbox-label input[type="checkbox"] {
		width: 16px;
		height: 16px;
		accent-color: var(--color-accent);
	}

	.pull-interval {
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.pull-interval:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.pull-interval:disabled {
		opacity: 0.5;
	}

	.modal-footer {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-3);
		padding: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	.btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.btn-secondary {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
	}

	.btn-secondary:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.btn-primary {
		background: var(--color-accent);
		border: none;
		color: white;
	}

	.btn-primary:hover:not(:disabled) {
		opacity: 0.9;
	}
</style>
