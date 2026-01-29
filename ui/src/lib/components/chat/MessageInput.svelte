<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';

	interface Props {
		onSend: (content: string) => Promise<void>;
		disabled?: boolean;
	}

	let { onSend, disabled = false }: Props = $props();

	let input = $state('');
	let sending = $state(false);
	let textarea: HTMLTextAreaElement;

	async function send(): Promise<void> {
		if (!input.trim() || sending || disabled) return;

		sending = true;
		const content = input;
		input = '';

		// Reset textarea height
		if (textarea) {
			textarea.style.height = 'auto';
		}

		try {
			await onSend(content);
		} finally {
			sending = false;
			// Re-focus for better UX
			textarea?.focus();
		}
	}

	function handleKeydown(e: KeyboardEvent): void {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			send();
		}
	}

	function handleInput(): void {
		// Auto-resize textarea
		if (textarea) {
			textarea.style.height = 'auto';
			textarea.style.height = Math.min(textarea.scrollHeight, 200) + 'px';
		}
	}
</script>

<div class="message-input-container">
	<div class="message-input">
		<textarea
			bind:this={textarea}
			bind:value={input}
			oninput={handleInput}
			onkeydown={handleKeydown}
			placeholder="Type a message..."
			rows="1"
			disabled={sending || disabled}
		></textarea>

		<button
			class="send-button"
			onclick={send}
			disabled={sending || disabled || !input.trim()}
			aria-label="Send message"
		>
			<Icon name={sending ? 'loader' : 'send'} size={20} />
		</button>
	</div>
</div>

<style>
	.message-input-container {
		position: relative;
		padding-top: var(--space-2);
	}

	.message-input {
		display: flex;
		align-items: flex-end;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
	}

	.message-input:focus-within {
		border-color: var(--color-accent);
	}

	textarea {
		flex: 1;
		resize: none;
		border: none;
		background: transparent;
		color: var(--color-text-primary);
		font-family: inherit;
		font-size: var(--font-size-base);
		line-height: var(--line-height-normal);
		min-height: 24px;
		max-height: 200px;
	}

	textarea:focus {
		outline: none;
	}

	textarea::placeholder {
		color: var(--color-text-muted);
	}

	textarea:disabled {
		opacity: 0.5;
	}

	.send-button {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		background: var(--color-accent);
		color: white;
		border: none;
		border-radius: var(--radius-md);
		transition: background var(--transition-fast);
		flex-shrink: 0;
	}

	.send-button:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.send-button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* Loader animation */
	.send-button :global(svg) {
		transition: transform 0.2s ease;
	}

	.send-button:not(:disabled):hover :global(svg) {
		transform: translateX(2px);
	}
</style>
