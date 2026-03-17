<script lang="ts">
	/**
	 * BottomChatBar - Persistent bottom panel that replaces the ChatDrawer overlay.
	 *
	 * Collapsed state (40px): Shows "Chat" label, message count badge, click to expand.
	 * Expanded state: Shows VerticalResizeHandle at top, then the existing ChatPanel content.
	 *
	 * Height is controlled by chatBarStore and persisted to localStorage.
	 */

	import { browser } from '$app/environment';
	import { chatBarStore } from '$lib/stores/chatDrawer.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';
	import VerticalResizeHandle from '$lib/components/layout/VerticalResizeHandle.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';

	const messageCount = $derived(messagesStore.messages.length);
	let viewportHeight = $state(browser ? window.innerHeight : 800);
</script>

<svelte:window onresize={() => (viewportHeight = window.innerHeight)} />

<div class="bottom-chat-bar" class:expanded={chatBarStore.expanded} data-testid="bottom-chat-bar">
	{#if chatBarStore.expanded}
		<VerticalResizeHandle
			onResize={(delta) => chatBarStore.setHeight(chatBarStore.height + delta)}
			valueNow={chatBarStore.height}
			valueMin={150}
			valueMax={Math.floor(viewportHeight * 0.6)}
		/>
	{/if}

	<!-- Collapsed bar / Header -->
	<button
		type="button"
		class="chat-header"
		onclick={() => chatBarStore.toggle()}
		aria-expanded={chatBarStore.expanded}
		aria-label={chatBarStore.expanded ? 'Collapse chat' : 'Expand chat'}
		data-testid="chat-bar-toggle"
	>
		<span class="header-icon" aria-hidden="true">
			{chatBarStore.expanded ? '▾' : '▴'}
		</span>
		<span class="header-label">Chat</span>

		{#if !chatBarStore.expanded && chatBarStore.pageContextItems.length > 0}
			<span class="context-hint" aria-label="Current context">
				{chatBarStore.pageContextItems.map((i) => i.label).join(', ')}
			</span>
		{/if}

		{#if messageCount > 0}
			<span class="header-count" aria-label="{messageCount} messages">{messageCount}</span>
		{/if}

		{#if !chatBarStore.expanded}
			<span class="header-hint">Click to chat</span>
		{/if}

		<span class="header-kbd" aria-label="Keyboard shortcut">Cmd+K</span>
	</button>

	{#if chatBarStore.expanded}
		<div
			class="chat-body"
			style="height: {chatBarStore.height}px"
			data-testid="chat-bar-body"
		>
			<ChatPanel
				title={chatBarStore.contextTitle}
				planSlug={chatBarStore.context.planSlug}
			/>
		</div>
	{/if}
</div>

<style>
	.bottom-chat-bar {
		flex-shrink: 0;
		border-top: 2px solid var(--color-accent);
		background: var(--color-bg-primary);
		display: flex;
		flex-direction: column;
		z-index: 50;
	}

	/* Header / collapsed bar */
	.chat-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: 0 var(--space-4);
		background: var(--color-bg-secondary);
		cursor: pointer;
		width: 100%;
		text-align: left;
		font-size: var(--font-size-sm);
		font-family: inherit;
		color: var(--color-text-primary);
		transition: background-color var(--transition-fast);
		min-height: 40px;
		border: none;
		border-radius: 0;
		appearance: none;
		user-select: none;
	}

	.chat-header:hover {
		background: var(--color-bg-tertiary);
	}

	.header-icon {
		font-size: 0.625rem;
		color: var(--color-accent);
	}

	.header-label {
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.context-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		max-width: 240px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.header-count {
		font-size: 0.625rem;
		padding: 1px 6px;
		border-radius: 9999px;
		background: var(--color-accent);
		color: white;
		font-weight: var(--font-weight-semibold);
		flex-shrink: 0;
	}

	.header-hint {
		flex: 1;
		text-align: right;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-weight: var(--font-weight-normal);
	}

	.header-kbd {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		padding: 1px 5px;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-family: var(--font-mono, monospace);
	}

	/* Expanded body */
	.chat-body {
		display: flex;
		flex-direction: column;
		overflow: hidden;
		min-height: 0;
	}

	/* When expanded, collapse the header-hint to give room to header-kbd */
	.expanded .header-hint {
		display: none;
	}

	/* Focus ring for keyboard navigation */
	.chat-header:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}
</style>
