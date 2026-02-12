<script lang="ts">
	import MessageList from '$lib/components/chat/MessageList.svelte';
	import MessageInput from '$lib/components/chat/MessageInput.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import { messagesStore } from '$lib/stores/messages.svelte';

	interface Props {
		title?: string;
	}

	let { title = 'Chat / Commands' }: Props = $props();

	const commandHints = [
		{ cmd: '/propose', desc: 'Start a new change' },
		{ cmd: '/status', desc: 'Check workflow status' },
		{ cmd: '/approve', desc: 'Approve a spec' },
		{ cmd: '/questions', desc: 'View pending questions' }
	];

	let showHints = $state(true);
</script>

<div class="chat-panel">
	<div class="panel-header">
		<h2 class="panel-title">{title}</h2>
		<button
			class="hints-toggle"
			onclick={() => (showHints = !showHints)}
			aria-label="{showHints ? 'Hide' : 'Show'} command hints"
		>
			<Icon name={showHints ? 'chevron-up' : 'chevron-down'} size={14} />
		</button>
	</div>

	{#if showHints && messagesStore.messages.length === 0}
		<div class="command-hints">
			<p class="hints-label">Quick commands:</p>
			<div class="hints-list">
				{#each commandHints as hint}
					<button
						class="hint-chip"
						onclick={() => messagesStore.send(hint.cmd)}
						title={hint.desc}
					>
						<code>{hint.cmd}</code>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<div class="chat-messages">
		<MessageList messages={messagesStore.messages} />
	</div>

	<div class="chat-input">
		<MessageInput
			onSend={(content) => messagesStore.send(content)}
			disabled={messagesStore.sending}
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
