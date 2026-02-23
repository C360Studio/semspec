<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { extractUrl, cleanUrl } from '$lib/constants/urls';
	import { VALID_DOCUMENT_EXTENSIONS } from '$lib/constants/fileTypes';

	interface Props {
		onSend: (content: string) => Promise<void>;
		disabled?: boolean;
		onUrlDetected?: (url: string | null) => void;
		onFilePathDetected?: (path: string | null) => void;
		/** Content to clear from input (set by parent after source added) */
		clearContent?: string | null;
		/** Called after content is cleared */
		onCleared?: () => void;
		/** Placeholder text for the input */
		placeholder?: string;
	}

	let {
		onSend,
		disabled = false,
		onUrlDetected,
		onFilePathDetected,
		clearContent = null,
		onCleared,
		placeholder = 'Type a message...'
	}: Props = $props();

	let input = $state('');
	let sending = $state(false);
	let textarea = $state<HTMLTextAreaElement | null>(null);
	let detectedUrl = $state<string | null>(null);
	let detectedFilePath = $state<string | null>(null);

	// Build file path pattern from centralized extensions
	const fileExtPattern = VALID_DOCUMENT_EXTENSIONS.map((e) => e.replace('.', '\\.')).join('|');
	const FILE_PATH_PATTERN = new RegExp(
		`(?:^|\\s)([~./][\\w/.~-]*(?:${fileExtPattern}))(?:\\s|$)`,
		'i'
	);

	// React to clearContent prop changes
	$effect(() => {
		if (clearContent) {
			input = input.replace(clearContent, '').trim();
			detectedUrl = null;
			detectedFilePath = null;
			onUrlDetected?.(null);
			onFilePathDetected?.(null);
			resizeTextarea();
			onCleared?.();
		}
	});

	async function send(): Promise<void> {
		if (!input.trim() || sending || disabled) return;

		sending = true;
		const content = input;
		input = '';

		// Reset detection state
		detectedUrl = null;
		detectedFilePath = null;
		onUrlDetected?.(null);
		onFilePathDetected?.(null);

		// Reset textarea height
		resizeTextarea();

		try {
			await onSend(content);
		} finally {
			sending = false;
			// Re-focus for better UX
			textarea?.focus();
		}
	}

	function resizeTextarea(): void {
		if (textarea) {
			textarea.style.height = 'auto';
			textarea.style.height = Math.min(textarea.scrollHeight, 200) + 'px';
		}
	}

	function handleKeydown(e: KeyboardEvent): void {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			send();
		}
	}

	function handleInput(): void {
		resizeTextarea();
		detectContent();
	}

	function detectContent(): void {
		// URL detection using utility function
		const foundUrl = extractUrl(input);
		if (foundUrl !== detectedUrl) {
			detectedUrl = foundUrl;
			onUrlDetected?.(detectedUrl);
		}

		// File path detection (only if no URL found)
		if (!detectedUrl) {
			const pathMatch = input.match(FILE_PATH_PATTERN);
			const newPath = pathMatch?.[1] ?? null;
			if (newPath !== detectedFilePath) {
				detectedFilePath = newPath;
				onFilePathDetected?.(detectedFilePath);
			}
		} else if (detectedFilePath !== null) {
			detectedFilePath = null;
			onFilePathDetected?.(null);
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
			{placeholder}
			rows="1"
			disabled={sending || disabled}
			aria-label="Message input"
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

	.send-button:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
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
