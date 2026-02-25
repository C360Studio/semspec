<script lang="ts">
	import Message from './Message.svelte';
	import ContextDivider from './ContextDivider.svelte';
	import type { Message as MessageType, MessageContext } from '$lib/types';

	interface Props {
		messages: MessageType[];
	}

	let { messages }: Props = $props();
	let container: HTMLDivElement;

	// Auto-scroll to bottom when messages change
	$effect(() => {
		if (messages.length > 0 && container) {
			// Use requestAnimationFrame to ensure DOM has updated
			requestAnimationFrame(() => {
				container.scrollTop = container.scrollHeight;
			});
		}
	});

	// Check if we should show a context divider between messages
	function shouldShowDivider(
		current: MessageType,
		previous: MessageType | undefined
	): boolean {
		// No divider for first message
		if (!previous) return false;

		// No divider if current message has no context
		if (!current.context) return false;

		// Show divider if previous message had no context
		if (!previous.context) return true;

		// Show divider if context changed
		return !contextsEqual(current.context, previous.context);
	}

	function contextsEqual(a: MessageContext, b: MessageContext): boolean {
		if (a.type !== b.type) return false;
		if (a.planSlug !== b.planSlug) return false;
		if (a.phaseId !== b.phaseId) return false;
		if (a.taskId !== b.taskId) return false;
		return true;
	}
</script>

<div
	class="message-list"
	bind:this={container}
	role="log"
	aria-live="polite"
	aria-label="Chat messages"
>
	{#if messages.length === 0}
		<div class="empty-state">
			<p class="empty-title">Start a conversation</p>
			<p class="empty-hint">Type a message below to get started</p>
		</div>
	{:else}
		{#each messages as message, i (message.id)}
			{#if shouldShowDivider(message, messages[i - 1])}
				<ContextDivider label={message.context?.label ?? ''} />
			{/if}
			<Message {message} />
		{/each}
	{/if}
</div>

<style>
	.message-list {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-4) 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.empty-state {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		color: var(--color-text-muted);
	}

	.empty-title {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-medium);
		margin-bottom: var(--space-2);
	}

	.empty-hint {
		font-size: var(--font-size-sm);
	}
</style>
