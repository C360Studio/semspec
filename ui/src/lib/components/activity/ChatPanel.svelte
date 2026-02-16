<script lang="ts">
	import { goto } from '$app/navigation';
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import SourceSuggestionChip from '$lib/components/chat/SourceSuggestionChip.svelte';
	import UploadModal from '$lib/components/sources/UploadModal.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { api } from '$lib/api/client';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import { projectStore } from '$lib/stores/project.svelte';
	import { isValidHttpUrl } from '$lib/constants/urls';
	import type { Message } from '$lib/types';
	import type { DocCategory } from '$lib/types/source';

	interface Props {
		title?: string;
		planSlug?: string;
	}

	let { title = 'Chat / Commands', planSlug }: Props = $props();

	const commandHints = [
		{ cmd: '/plan', desc: 'Create a new plan' },
		{ cmd: '/approve', desc: 'Approve a draft plan' },
		{ cmd: '/tasks', desc: 'View tasks for a plan' },
		{ cmd: '/execute', desc: 'Execute a plan' },
		{ cmd: '/source', desc: 'Add a source (URL or upload)' },
		{ cmd: '/help', desc: 'Show available commands' }
	];

	let showHints = $state(true);
	let detectedUrl = $state<string | null>(null);
	let detectedFilePath = $state<string | null>(null);
	let showUploadModal = $state(false);
	let suggestedFilename = $state<string | undefined>(undefined);

	// State for clearing content from input (prop-based communication)
	let clearContent = $state<string | null>(null);

	// Resolve project ID from context with safe fallback
	const projectId = $derived.by(() => {
		if (planSlug) {
			const plan = plansStore.getBySlug(planSlug);
			return plan?.projectId ?? 'default';
		}
		return projectStore.currentProjectId ?? 'default';
	});

	// Get plan's loop IDs for filtering
	const planLoopIds = $derived.by(() => {
		if (!planSlug) return null;
		const plan = plansStore.getBySlug(planSlug);
		return plan?.active_loops.map((l) => l.loop_id) ?? [];
	});

	// Filter messages to plan's loops if planSlug is provided
	const filteredMessages = $derived.by((): Message[] => {
		if (!planSlug || !planLoopIds) {
			return messagesStore.messages;
		}
		// Show messages that either:
		// 1. Have no loopId (global messages like user input)
		// 2. Have a loopId matching one of the plan's loops
		return messagesStore.messages.filter(
			(m) => !m.loopId || planLoopIds.includes(m.loopId)
		);
	});

	// Slug validation pattern: lowercase alphanumeric with hyphens
	const slugPattern = /^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$/;

	/**
	 * Validate slug format client-side for better UX.
	 */
	function isValidSlug(slug: string): boolean {
		return slugPattern.test(slug) && slug.length <= 50;
	}

	/**
	 * Handle message send with slash command interception.
	 * Commands are handled via REST API, regular messages go to backend.
	 */
	async function handleSend(content: string): Promise<void> {
		// /plan <description> - Create a new plan
		if (content.startsWith('/plan ')) {
			const description = content.slice(6).trim();
			if (!description) {
				messagesStore.addStatus('Usage: /plan <description>');
				return;
			}
			try {
				messagesStore.addStatus(`Creating plan: ${description}...`);
				const result = await api.plans.create({ description });
				messagesStore.addStatus(`Plan created: ${result.slug}`);
				clearContent = content; // Clear input on success
				await goto(`/plans/${result.slug}`);
			} catch (err) {
				const errorMsg = err instanceof Error ? err.message : 'Unknown error';
				messagesStore.addStatus(`Failed to create plan: ${errorMsg}`);
			}
			return;
		}

		// /approve <slug> - Approve a draft plan
		if (content.startsWith('/approve ')) {
			const slug = content.slice(9).trim();
			if (!slug) {
				messagesStore.addStatus('Usage: /approve <slug>');
				return;
			}
			if (!isValidSlug(slug)) {
				messagesStore.addStatus('Invalid slug format. Use lowercase letters, numbers, and hyphens.');
				return;
			}
			try {
				messagesStore.addStatus(`Approving plan: ${slug}...`);
				await api.plans.promote(slug);
				messagesStore.addStatus(`Plan approved: ${slug}`);
				clearContent = content; // Clear input on success
				await plansStore.fetch();
			} catch (err) {
				const errorMsg = err instanceof Error ? err.message : 'Unknown error';
				messagesStore.addStatus(`Failed to approve plan: ${errorMsg}`);
			}
			return;
		}

		// /tasks <slug> - View tasks for a plan
		if (content.startsWith('/tasks ')) {
			const slug = content.slice(7).trim();
			if (!slug) {
				messagesStore.addStatus('Usage: /tasks <slug>');
				return;
			}
			if (!isValidSlug(slug)) {
				messagesStore.addStatus('Invalid slug format. Use lowercase letters, numbers, and hyphens.');
				return;
			}
			clearContent = content; // Clear input on success
			await goto(`/plans/${slug}`);
			return;
		}

		// /execute <slug> - Execute a plan
		if (content.startsWith('/execute ')) {
			const slug = content.slice(9).trim();
			if (!slug) {
				messagesStore.addStatus('Usage: /execute <slug>');
				return;
			}
			if (!isValidSlug(slug)) {
				messagesStore.addStatus('Invalid slug format. Use lowercase letters, numbers, and hyphens.');
				return;
			}
			try {
				messagesStore.addStatus(`Executing plan: ${slug}...`);
				await api.plans.execute(slug);
				messagesStore.addStatus(`Plan execution started: ${slug}`);
				clearContent = content; // Clear input on success
				await goto(`/plans/${slug}`);
			} catch (err) {
				const errorMsg = err instanceof Error ? err.message : 'Unknown error';
				messagesStore.addStatus(`Failed to execute plan: ${errorMsg}`);
			}
			return;
		}

		// /source - Add a source
		if (content.startsWith('/source ')) {
			await handleSourceCommand(content);
			return;
		}

		// /help - Show available commands
		if (content === '/help') {
			showHelpMessage();
			clearContent = content; // Clear input on success
			return;
		}

		// Send regular message to backend
		await messagesStore.send(content);
	}

	/**
	 * Display help message with available commands.
	 */
	function showHelpMessage(): void {
		messagesStore.addStatus(`Available commands:
• /plan <description> - Create a new plan
• /approve <slug> - Approve a draft plan
• /tasks <slug> - View tasks for a plan
• /execute <slug> - Execute a plan
• /source <url> - Add a web source
• /source upload - Upload a file
• /help - Show this help`);
	}

	/**
	 * Handle /source command with URL validation.
	 */
	async function handleSourceCommand(content: string): Promise<void> {
		const args = content.slice('/source '.length).trim();

		if (args === 'upload') {
			showUploadModal = true;
			return;
		}

		// Parse: /source <url>
		const url = args.trim();

		if (!url) {
			messagesStore.addStatus('Usage: /source <url> or /source upload');
			return;
		}

		if (!url.startsWith('http')) {
			messagesStore.addStatus('Invalid URL: must start with http:// or https://');
			return;
		}

		// Validate URL format
		if (!isValidHttpUrl(url)) {
			messagesStore.addStatus('Invalid URL format. Please check the URL and try again.');
			return;
		}

		try {
			const result = await sourcesStore.addWebSource({ url, projectId });
			if (result) {
				messagesStore.addStatus(`Added source: ${result.name}`);
			} else {
				const errorMsg = sourcesStore.error || 'Unknown error';
				messagesStore.addStatus(`Failed to add source: ${errorMsg}`);
				console.error('Failed to add web source:', errorMsg);
			}
		} catch (err) {
			const errorMsg = err instanceof Error ? err.message : 'Unexpected error';
			messagesStore.addStatus(`Failed to add source: ${errorMsg}`);
			console.error('Error adding web source:', err);
		}
	}

	/**
	 * Handle adding URL as web source from suggestion chip.
	 */
	async function handleAddUrl(): Promise<void> {
		if (!detectedUrl) return;

		// Validate URL before sending
		if (!isValidHttpUrl(detectedUrl)) {
			messagesStore.addStatus('Invalid URL format');
			detectedUrl = null;
			return;
		}

		const url = detectedUrl;
		try {
			const result = await sourcesStore.addWebSource({ url, projectId });
			if (result) {
				// Clear the URL from input using prop-based communication
				clearContent = url;
				detectedUrl = null;
				messagesStore.addStatus(`Added source: ${result.name}`);
			} else {
				const errorMsg = sourcesStore.error || 'Unknown error';
				messagesStore.addStatus(`Failed to add source: ${errorMsg}`);
				console.error('Failed to add web source:', errorMsg);
			}
		} catch (err) {
			const errorMsg = err instanceof Error ? err.message : 'Unexpected error';
			messagesStore.addStatus(`Failed to add source: ${errorMsg}`);
			console.error('Error adding web source:', err);
		}
	}

	/**
	 * Handle file path suggestion - open upload modal.
	 */
	async function handleAddFilePath(): Promise<void> {
		if (!detectedFilePath) return;

		suggestedFilename = detectedFilePath.split('/').pop();
		clearContent = detectedFilePath;
		detectedFilePath = null;
		showUploadModal = true;
	}

	/**
	 * Handle file upload from modal.
	 */
	function handleUpload(file: File, options: { category: DocCategory; project?: string }): void {
		sourcesStore
			.upload(file, {
				projectId: options.project || projectId,
				category: options.category
			})
			.then((result) => {
				if (result) {
					messagesStore.addStatus(`Uploaded: ${result.name}`);
				} else {
					const errorMsg = sourcesStore.error || 'Unknown error';
					messagesStore.addStatus(`Upload failed: ${errorMsg}`);
					console.error('Failed to upload file:', errorMsg);
				}
			})
			.catch((err) => {
				const errorMsg = err instanceof Error ? err.message : 'Unexpected error';
				messagesStore.addStatus(`Upload failed: ${errorMsg}`);
				console.error('Error uploading file:', err);
			});
		showUploadModal = false;
		suggestedFilename = undefined;
	}

	/**
	 * Called when content is cleared from input.
	 */
	function handleCleared(): void {
		clearContent = null;
	}
</script>

<div class="chat-panel">
	<div class="panel-header">
		<h2 class="panel-title">{title}</h2>
		<button
			class="hints-toggle"
			onclick={() => (showHints = !showHints)}
			aria-label="{showHints ? 'Hide' : 'Show'} command hints"
			aria-expanded={showHints}
		>
			<Icon name={showHints ? 'chevron-up' : 'chevron-down'} size={14} />
		</button>
	</div>

	{#if showHints && messagesStore.messages.length === 0}
		<div class="command-hints" role="region" aria-label="Quick commands">
			<p class="hints-label">Quick commands:</p>
			<div class="hints-list">
				{#each commandHints as hint}
					<button
						class="hint-chip"
						onclick={async () => await handleSend(hint.cmd)}
						title={hint.desc}
					>
						<code>{hint.cmd}</code>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<div class="chat-messages">
		<MessageList messages={filteredMessages} />
	</div>

	<div class="chat-input">
		{#if detectedUrl}
			<SourceSuggestionChip
				type="url"
				value={detectedUrl}
				{projectId}
				onAdd={handleAddUrl}
				onDismiss={() => (detectedUrl = null)}
			/>
		{:else if detectedFilePath}
			<SourceSuggestionChip
				type="file"
				value={detectedFilePath}
				{projectId}
				onAdd={handleAddFilePath}
				onDismiss={() => (detectedFilePath = null)}
			/>
		{/if}
		<MessageInput
			onSend={handleSend}
			disabled={messagesStore.sending}
			onUrlDetected={(url) => (detectedUrl = url)}
			onFilePathDetected={(path) => (detectedFilePath = path)}
			{clearContent}
			onCleared={handleCleared}
		/>
	</div>
</div>

<UploadModal
	open={showUploadModal}
	uploading={sourcesStore.uploading}
	progress={sourcesStore.uploadProgress}
	onclose={() => {
		showUploadModal = false;
		suggestedFilename = undefined;
	}}
	onupload={handleUpload}
/>

<style>
	.chat-panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
	}

	.panel-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding-bottom: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		margin-bottom: var(--space-3);
	}

	.panel-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.hints-toggle {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
	}

	.hints-toggle:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.hints-toggle:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.command-hints {
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		margin-bottom: var(--space-3);
	}

	.hints-label {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		margin: 0 0 var(--space-2);
	}

	.hints-list {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.hint-chip {
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.hint-chip:hover {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
	}

	.hint-chip:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.hint-chip code {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-primary);
	}

	.chat-messages {
		flex: 1;
		overflow-y: auto;
		min-height: 0;
	}

	.chat-input {
		flex-shrink: 0;
		padding-top: var(--space-2);
		border-top: 1px solid var(--color-border);
	}
</style>
