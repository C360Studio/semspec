<script lang="ts">
	import type { ChatMode } from '$lib/stores/chatMode.svelte';

	interface Props {
		mode: ChatMode;
		label: string;
	}

	let { mode, label }: Props = $props();

	const icons: Record<ChatMode, string> = {
		chat: '💬',
		plan: '📋',
		execute: '⚡'
	};
</script>

<div class="mode-indicator" data-testid="mode-indicator" data-mode={mode}>
	<span class="mode-icon">{icons[mode]}</span>
	<span class="mode-label">{label}</span>
</div>

<style>
	.mode-indicator {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: var(--color-surface-secondary);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
	}

	.mode-icon {
		font-size: var(--font-size-sm);
	}

	.mode-label {
		font-weight: var(--font-weight-medium);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	/* Mode-specific colors */
	.mode-indicator[data-mode='plan'] {
		background: var(--color-primary-subtle);
		color: var(--color-primary);
	}

	.mode-indicator[data-mode='execute'] {
		background: var(--color-success-subtle);
		color: var(--color-success);
	}
</style>
