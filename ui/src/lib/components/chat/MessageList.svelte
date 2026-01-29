<script lang="ts">
	import Message from './Message.svelte';
	import type { Message as MessageType } from '$lib/types';

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
		{#each messages as message (message.id)}
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
