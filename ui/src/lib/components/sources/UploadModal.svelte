<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { DocCategory } from '$lib/types/source';
	import { CATEGORY_META } from '$lib/types/source';

	interface Props {
		open: boolean;
		uploading?: boolean;
		progress?: number;
		onclose: () => void;
		onupload: (file: File, options: { category: DocCategory; project?: string }) => void;
	}

	let { open, uploading = false, progress = 0, onclose, onupload }: Props = $props();

	let selectedFile = $state<File | null>(null);
	let selectedCategory = $state<DocCategory>('reference');
	let project = $state('');
	let dragOver = $state(false);
	let fileInput: HTMLInputElement;

	const categories = Object.entries(CATEGORY_META) as [DocCategory, { label: string; color: string; icon: string }][];

	function handleDragOver(e: DragEvent) {
		e.preventDefault();
		dragOver = true;
	}

	function handleDragLeave() {
		dragOver = false;
	}

	function handleDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;

		const files = e.dataTransfer?.files;
		if (files && files.length > 0) {
			selectFile(files[0]);
		}
	}

	function handleFileSelect(e: Event) {
		const target = e.target as HTMLInputElement;
		if (target.files && target.files.length > 0) {
			selectFile(target.files[0]);
		}
	}

	function selectFile(file: File) {
		selectedFile = file;

		// Auto-detect category from extension
		const ext = file.name.split('.').pop()?.toLowerCase();
		if (ext === 'md' || ext === 'txt') {
			// Check filename for hints
			const name = file.name.toLowerCase();
			if (name.includes('sop') || name.includes('procedure')) {
				selectedCategory = 'sop';
			} else if (name.includes('spec') || name.includes('specification')) {
				selectedCategory = 'spec';
			} else if (name.includes('api')) {
				selectedCategory = 'api';
			}
		}
	}

	function handleSubmit() {
		if (!selectedFile) return;
		onupload(selectedFile, {
			category: selectedCategory,
			project: project.trim() || undefined
		});
	}

	function handleClose() {
		if (!uploading) {
			selectedFile = null;
			selectedCategory = 'reference';
			project = '';
			onclose();
		}
	}

	function openFilePicker() {
		fileInput.click();
	}

	function formatFileSize(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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
			aria-labelledby="upload-title"
			tabindex="-1"
		>
			<header class="modal-header">
				<h2 id="upload-title">Upload Document</h2>
				<button
					class="close-button"
					onclick={handleClose}
					disabled={uploading}
					aria-label="Close"
				>
					<Icon name="x" size={20} />
				</button>
			</header>

			<div class="modal-body">
				{#if !selectedFile}
					<div
						class="drop-zone"
						class:drag-over={dragOver}
						ondragover={handleDragOver}
						ondragleave={handleDragLeave}
						ondrop={handleDrop}
						role="button"
						tabindex="0"
						onclick={openFilePicker}
						onkeydown={(e) => e.key === 'Enter' && openFilePicker()}
					>
						<Icon name="upload-cloud" size={48} />
						<p class="drop-text">Drag and drop a file here</p>
						<p class="drop-subtext">or click to browse</p>
						<p class="file-types">Supports: .md, .txt, .pdf</p>
					</div>
					<input
						bind:this={fileInput}
						type="file"
						accept=".md,.txt,.pdf"
						onchange={handleFileSelect}
						class="hidden-input"
					/>
				{:else}
					<div class="file-preview">
						<div class="file-info">
							<Icon name="file-text" size={24} />
							<div class="file-details">
								<span class="file-name">{selectedFile.name}</span>
								<span class="file-size">{formatFileSize(selectedFile.size)}</span>
							</div>
							{#if !uploading}
								<button
									class="remove-file"
									onclick={() => (selectedFile = null)}
									aria-label="Remove file"
								>
									<Icon name="x" size={16} />
								</button>
							{/if}
						</div>

						{#if uploading}
							<div class="progress-bar">
								<div class="progress-fill" style="width: {progress}%"></div>
							</div>
							<p class="progress-text">Uploading... {progress}%</p>
						{/if}
					</div>

					{#if !uploading}
						<div class="form-fields">
							<div class="form-group">
								<label for="category">Category</label>
								<div class="category-select">
									{#each categories as [value, meta]}
										<button
											type="button"
											class="category-option"
											class:selected={selectedCategory === value}
											onclick={() => (selectedCategory = value)}
											style="--category-color: {meta.color}"
										>
											<Icon name={meta.icon} size={16} />
											<span>{meta.label}</span>
										</button>
									{/each}
								</div>
							</div>

							<div class="form-group">
								<label for="project">Project Tag (optional)</label>
								<input
									id="project"
									type="text"
									bind:value={project}
									placeholder="e.g., auth-system"
								/>
							</div>
						</div>
					{/if}
				{/if}
			</div>

			<footer class="modal-footer">
				<button class="btn btn-secondary" onclick={handleClose} disabled={uploading}>
					Cancel
				</button>
				<button
					class="btn btn-primary"
					onclick={handleSubmit}
					disabled={!selectedFile || uploading}
				>
					{#if uploading}
						<Icon name="loader" size={16} />
						Uploading...
					{:else}
						<Icon name="upload" size={16} />
						Upload
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

	.drop-zone {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: var(--space-8);
		border: 2px dashed var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
		color: var(--color-text-muted);
	}

	.drop-zone:hover,
	.drop-zone.drag-over {
		border-color: var(--color-accent);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.drop-text {
		margin: var(--space-2) 0 0;
		font-weight: var(--font-weight-medium);
	}

	.drop-subtext {
		margin: var(--space-1) 0 0;
		font-size: var(--font-size-sm);
	}

	.file-types {
		margin: var(--space-3) 0 0;
		font-size: var(--font-size-xs);
	}

	.hidden-input {
		display: none;
	}

	.file-preview {
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
	}

	.file-info {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.file-details {
		flex: 1;
		min-width: 0;
	}

	.file-name {
		display: block;
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.file-size {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.remove-file {
		padding: var(--space-1);
		background: none;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
	}

	.remove-file:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-error);
	}

	.progress-bar {
		margin-top: var(--space-3);
		height: 4px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--color-accent);
		transition: width 0.2s ease;
	}

	.progress-text {
		margin: var(--space-2) 0 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		text-align: center;
	}

	.form-fields {
		margin-top: var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.form-group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.form-group label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.form-group input {
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

	.category-select {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.category-option {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.category-option:hover {
		border-color: var(--category-color);
		color: var(--category-color);
	}

	.category-option.selected {
		background: color-mix(in srgb, var(--category-color) 15%, transparent);
		border-color: var(--category-color);
		color: var(--category-color);
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
