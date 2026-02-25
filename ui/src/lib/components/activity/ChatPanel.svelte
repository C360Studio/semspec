<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import ModeIndicator from '$lib/components/chat/ModeIndicator.svelte';
	import SourceSuggestionChip from '$lib/components/chat/SourceSuggestionChip.svelte';
	import UploadModal from '$lib/components/sources/UploadModal.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { sourcesStore } from '$lib/stores/sources.svelte';
	import { projectStore } from '$lib/stores/project.svelte';
	import { getChatModeConfig, type ChatModeConfig } from '$lib/stores/chatMode.svelte';
	import { api } from '$lib/api/client';
	import { isValidHttpUrl } from '$lib/constants/urls';
	import type { Message, MessageContext } from '$lib/types';
	import type { DocCategory } from '$lib/types/source';
	import type { PlanSelection } from '$lib/stores/planSelection.svelte';

	interface Props {
		title?: string;
		planSlug?: string;
		/** Selection context from plan nav tree - attached to messages */
		selectionContext?: PlanSelection | null;
		/** Label resolver for selection context */
		getContextLabel?: (selection: PlanSelection) => string;
	}

	let { title = 'Chat', planSlug, selectionContext, getContextLabel }: Props = $props();

	// Build MessageContext from selection
	function buildMessageContext(): MessageContext | undefined {
		if (!selectionContext) return undefined;

		const label = getContextLabel?.(selectionContext) ?? selectionContext.planSlug;

		return {
			type: selectionContext.type,
			planSlug: selectionContext.planSlug,
			phaseId: selectionContext.phaseId,
			taskId: selectionContext.taskId,
			label
		};
	}

	// Get current chat mode based on route context
	const modeConfig = $derived(getChatModeConfig($page.url.pathname, planSlug));

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
			return plan?.project_id ?? 'default';
		}
		return projectStore.currentProjectId ?? 'default';
	});

	// Get plan's loop IDs for filtering
	const planLoopIds = $derived.by(() => {
		if (!planSlug) return null;
		const plan = plansStore.getBySlug(planSlug);
		return (plan?.active_loops ?? []).map((l) => l.loop_id);
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

	/**
	 * Handle message send - routes to appropriate endpoint based on mode.
	 */
	async function handleSend(content: string): Promise<void> {
		if (!content.trim()) return;

		// Build context from current selection
		const context = buildMessageContext();

		// Add user message immediately
		const userMessage: Message = {
			id: crypto.randomUUID(),
			type: 'user',
			content,
			timestamp: new Date().toISOString(),
			context
		};
		messagesStore.messages = [...messagesStore.messages, userMessage];

		try {
			switch (modeConfig.mode) {
				case 'plan': {
					// Create plan via workflow API
					messagesStore.addStatus('Creating plan...');
					const result = await api.plans.create({ description: content });
					messagesStore.addStatus(`Plan created: ${result.slug}`);
					// Navigate to the new plan
					await goto(`/plans/${result.slug}`);
					break;
				}
				case 'execute': {
					// Execute plan via workflow API
					if (!planSlug) {
						messagesStore.addStatus('Error: No plan selected for execution');
						return;
					}
					messagesStore.addStatus('Starting execution...');
					await api.plans.execute(planSlug);
					messagesStore.addStatus('Execution started');
					// Refresh plans to show updated state
					await plansStore.fetch();
					break;
				}
				case 'chat':
				default: {
					// For chat mode, messagesStore.send handles the full flow
					// Remove the user message we added since messagesStore.send adds it
					messagesStore.messages = messagesStore.messages.slice(0, -1);
					await messagesStore.send(content);
					break;
				}
			}
		} catch (err) {
			const errorMessage: Message = {
				id: crypto.randomUUID(),
				type: 'error',
				content: err instanceof Error ? err.message : 'Failed to process request',
				timestamp: new Date().toISOString()
			};
			messagesStore.messages = [...messagesStore.messages, errorMessage];
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
	</div>

	<div class="chat-messages">
		<MessageList messages={filteredMessages} />
	</div>

	<div class="chat-input">
		<div class="mode-row">
			<ModeIndicator mode={modeConfig.mode} label={modeConfig.label} />
		</div>
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
			placeholder={modeConfig.hint}
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

	.mode-row {
		margin-bottom: var(--space-2);
	}
</style>
