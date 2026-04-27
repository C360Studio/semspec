<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import ModeIndicator from '$lib/components/chat/ModeIndicator.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import { invalidate } from '$app/navigation';
	import type { PlanWithStatus } from '$lib/types/plan';
	import { getChatModeConfig } from '$lib/stores/chatMode.svelte';
	import { api } from '$lib/api/client';
	import type { Message, MessageContext } from '$lib/types';
	import type { PlanSelection } from '$lib/stores/planSelection.svelte';

	interface Props {
		title?: string;
		planSlug?: string;
		/** Plan object for context (projectId, active loops). Passed from parent. */
		plan?: PlanWithStatus | null;
		/** Selection context from plan nav tree - attached to messages */
		selectionContext?: PlanSelection | null;
		/** Label resolver for selection context */
		getContextLabel?: (selection: PlanSelection) => string;
	}

	let { title = 'Chat', planSlug, plan = null, selectionContext, getContextLabel }: Props = $props();

	// Build MessageContext from selection
	function buildMessageContext(): MessageContext | undefined {
		if (!selectionContext) return undefined;

		const label = getContextLabel?.(selectionContext) ?? selectionContext.planSlug;

		return {
			type: selectionContext.type,
			planSlug: selectionContext.planSlug,
			phaseId: selectionContext.phaseId,
			taskId: selectionContext.taskId,
			requirementId: selectionContext.requirementId,
			scenarioId: selectionContext.scenarioId,
			label
		};
	}

	// Get current chat mode based on route context
	const modeConfig = $derived(getChatModeConfig(page.url.pathname, planSlug, plan));

	// Get plan's loop IDs for filtering
	const planLoopIds = $derived(
		plan ? (plan.active_loops ?? []).map((l) => l.loop_id) : null
	);

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
					await invalidate('app:plans');
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
		<MessageInput
			onSend={handleSend}
			disabled={messagesStore.sending}
			placeholder={modeConfig.hint}
		/>
	</div>
</div>

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
