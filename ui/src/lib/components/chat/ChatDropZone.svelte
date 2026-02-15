<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import {
		isValidDocumentFile,
		SUPPORTED_FILES_DESCRIPTION
	} from '$lib/constants/fileTypes';

	interface Props {
		projectId: string;
		children: Snippet;
	}

	let { projectId, children }: Props = $props();

	let dragOver = $state(false);
	let dragCounter = $state(0);
	let statusMessage = $state<string | null>(null);

	function handleDragEnter(e: DragEvent): void {
		e.preventDefault();
		dragCounter++;
		if (e.dataTransfer?.types.includes('Files')) {
			dragOver = true;
		}
	}

	function handleDragLeave(e: DragEvent): void {
		e.preventDefault();
		dragCounter--;
		if (dragCounter === 0) {
			dragOver = false;
		}
	}

	function handleDragOver(e: DragEvent): void {
		e.preventDefault();
	}

	async function handleDrop(e: DragEvent): Promise<void> {
		e.preventDefault();
		dragOver = false;
		dragCounter = 0;

		const files = e.dataTransfer?.files;
		if (!files || files.length === 0) {
			return;
		}

		const file = files[0];

		// Validate file type using centralized utility
		if (!isValidDocumentFile(file)) {
			const message = `Invalid file type. ${SUPPORTED_FILES_DESCRIPTION}`;
			messagesStore.addStatus(message);
			statusMessage = message;
			// Clear status after announcement
			setTimeout(() => {
				statusMessage = null;
			}, 3000);
			return;
		}

		// Validate projectId
		if (!projectId) {
			const message = 'Cannot upload: project context not available';
			messagesStore.addStatus(message);
			statusMessage = message;
			setTimeout(() => {
				statusMessage = null;
			}, 3000);
			console.error('ChatDropZone: projectId is required for upload');
			return;
		}

		try {
			statusMessage = `Uploading ${file.name}...`;
			const result = await sourcesStore.upload(file, { projectId });

			if (result) {
				const message = `Uploaded: ${result.name}`;
				messagesStore.addStatus(message);
				statusMessage = message;
			} else {
				const errorMsg = sourcesStore.error || 'Unknown error';
				const message = `Upload failed: ${errorMsg}`;
				messagesStore.addStatus(message);
				statusMessage = message;
				console.error('Failed to upload file:', errorMsg);
			}
		} catch (err) {
			const errorMsg = err instanceof Error ? err.message : 'Unexpected error';
			const message = `Upload failed: ${errorMsg}`;
			messagesStore.addStatus(message);
			statusMessage = message;
			console.error('Error uploading file:', err);
		} finally {
			// Clear status after a delay
			setTimeout(() => {
				statusMessage = null;
			}, 3000);
		}
	}
</script>

<div
	class="drop-zone-container"
	ondragenter={handleDragEnter}
	ondragleave={handleDragLeave}
	ondragover={handleDragOver}
	ondrop={handleDrop}
	role="region"
	aria-label="Drop files here to upload"
>
	{@render children()}

	{#if dragOver}
		<div class="drop-overlay" aria-hidden="true">
			<div class="drop-content">
				<Icon name="upload" size={48} />
				<p>Drop file to upload as source</p>
				<p class="file-types">{SUPPORTED_FILES_DESCRIPTION}</p>
			</div>
		</div>
	{/if}

	<!-- Live region for screen reader announcements -->
	<div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
		{#if dragOver}
			Drop zone active. Release to upload file.
		{:else if statusMessage}
			{statusMessage}
		{/if}
	</div>
</div>

<style>
	.drop-zone-container {
		position: relative;
		height: 100%;
		width: 100%;
	}

	.drop-overlay {
		position: absolute;
		inset: 0;
		background: color-mix(in srgb, var(--color-accent) 10%, var(--color-bg-primary) 90%);
		border: 2px dashed var(--color-accent);
		border-radius: var(--radius-md);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 100;
		pointer-events: none;
	}

	.drop-content {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-accent);
		text-align: center;
	}

	.drop-content p {
		margin: 0;
		font-weight: var(--font-weight-medium);
	}

	.file-types {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	/* Screen reader only - visually hidden but accessible */
	.sr-only {
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
