<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import { REFRESH_INTERVAL_OPTIONS } from '$lib/types/source';
	import type { AddWebSourceRequest } from '$lib/types/source';

	interface Props {
		open: boolean;
		loading?: boolean;
		onclose: () => void;
		onsubmit: (request: AddWebSourceRequest) => void;
	}

	let { open, loading = false, onclose, onsubmit }: Props = $props();

	let url = $state('');
	let project = $state('');
	let autoRefresh = $state(false);
	let refreshInterval = $state('');
	let urlError = $state<string | null>(null);
	let dialogElement: HTMLElement | null = $state(null);

	const isValidUrl = $derived(Boolean(url) && url.startsWith('https://') && !urlError);

	// Focus the modal when it opens
	$effect(() => {
		if (open && dialogElement) {
			dialogElement.focus();
		}
	});

	function validateUrl() {
		if (!url) {
			urlError = 'URL is required';
		} else if (!url.startsWith('https://')) {
			urlError = 'Only HTTPS URLs are allowed';
		} else {
			try {
				new URL(url);
				urlError = null;
			} catch {
				urlError = 'Invalid URL format';
			}
		}
	}

	function handleSubmit() {
		validateUrl();
		if (urlError) return;

		onsubmit({
			url: url.trim(),
			projectId: project.trim() || 'default',
			autoRefresh,
			refreshInterval: autoRefresh ? refreshInterval : undefined
		});
	}

	function handleClose() {
		if (!loading) {
			url = '';
			project = '';
			autoRefresh = false;
			refreshInterval = '';
			urlError = null;
			onclose();
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && !loading) {
			handleClose();
		}
	}

	// Only validate on input after first error to avoid aggressive validation
	function handleUrlInput() {
		if (urlError) {
			validateUrl();
		}
	}
</script>

<svelte:window onkeydown={open ? handleKeydown : undefined} />

{#if open}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div class="modal-backdrop" onclick={handleClose} role="presentation">
		<div
			bind:this={dialogElement}
			class="modal"
			onclick={(e) => e.stopPropagation()}
			role="dialog"
			aria-modal="true"
			aria-labelledby="add-web-title"
			tabindex="-1"
		>
			<header class="modal-header">
				<h2 id="add-web-title">Add Web Source</h2>
				<button
					class="close-button"
					onclick={handleClose}
					disabled={loading}
					aria-label="Close modal"
					type="button"
				>
					<Icon name="x" size={20} />
				</button>
			</header>

			<div class="modal-body">
				<div class="form-group">
					<label for="web-url">URL <span class="required" aria-label="required">*</span></label>
					<input
						id="web-url"
						type="url"
						bind:value={url}
						oninput={handleUrlInput}
						onblur={validateUrl}
						placeholder="https://docs.example.com/guide"
						class:error={urlError}
						disabled={loading}
						aria-invalid={!!urlError}
						aria-describedby={urlError ? 'url-error' : 'url-hint'}
					/>
					{#if urlError}
						<span id="url-error" class="error-message" role="alert">{urlError}</span>
					{/if}
					<span id="url-hint" class="hint">Enter a public HTTPS URL to documentation or reference pages</span>
				</div>

				<div class="form-group">
					<label for="web-project">Project Tag (optional)</label>
					<input
						id="web-project"
						type="text"
						bind:value={project}
						placeholder="e.g., auth-docs"
						disabled={loading}
						aria-describedby="project-hint"
					/>
					<span id="project-hint" class="hint">Group related sources together</span>
				</div>

				<div class="form-group auto-refresh-group">
					<label for="auto-refresh-toggle" class="checkbox-label">
						<input
							id="auto-refresh-toggle"
							type="checkbox"
							bind:checked={autoRefresh}
							disabled={loading}
							aria-describedby="auto-refresh-hint"
						/>
						<span>Auto-refresh content</span>
					</label>

					{#if autoRefresh}
						<label for="refresh-interval" class="visually-hidden">Refresh interval</label>
						<select
							id="refresh-interval"
							bind:value={refreshInterval}
							class="refresh-interval"
							disabled={loading}
							aria-label="Refresh interval"
						>
							{#each REFRESH_INTERVAL_OPTIONS as option}
								<option value={option.value}>{option.label}</option>
							{/each}
						</select>
					{/if}
					<span id="auto-refresh-hint" class="visually-hidden">Enable automatic content refresh at regular intervals</span>
				</div>

				<div class="info-box" role="note">
					<Icon name="info" size={16} />
					<p>The page will be fetched, converted to markdown, and indexed for context assembly. Only public HTTPS URLs are supported.</p>
				</div>
			</div>

			<footer class="modal-footer">
				<button class="btn btn-secondary" onclick={handleClose} disabled={loading} type="button">
					Cancel
				</button>
				<button
					class="btn btn-primary"
					onclick={handleSubmit}
					disabled={!isValidUrl || loading}
					type="button"
				>
					{#if loading}
						<Icon name="loader" size={16} />
						Adding...
					{:else}
						<Icon name="globe" size={16} />
						Add URL
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

	.modal:focus {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
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

	.form-group input[type='text'],
	.form-group input[type='url'] {
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

	.auto-refresh-group {
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

	.checkbox-label input[type='checkbox'] {
		width: 16px;
		height: 16px;
		accent-color: var(--color-accent);
	}

	.refresh-interval {
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.refresh-interval:focus {
		outline: none;
		border-color: var(--color-accent);
	}

	.refresh-interval:disabled {
		opacity: 0.5;
	}

	.info-box {
		display: flex;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-info)10;
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		margin-top: var(--space-4);
	}

	.info-box p {
		margin: 0;
		font-size: var(--font-size-sm);
		line-height: 1.4;
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

	/* Visually hidden but accessible to screen readers */
	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}
</style>
