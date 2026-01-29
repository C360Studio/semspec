<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	interface Props {
		onClose: () => void;
		title?: string;
		children: Snippet;
	}

	let { onClose, title, children }: Props = $props();

	function handleKeydown(event: KeyboardEvent): void {
		if (event.key === 'Escape') {
			onClose();
		}
	}

	function handleBackdropClick(event: MouseEvent): void {
		if (event.target === event.currentTarget) {
			onClose();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<div
	class="modal-backdrop"
	onclick={handleBackdropClick}
	onkeydown={(e) => e.key === 'Enter' && handleBackdropClick(e as unknown as MouseEvent)}
	role="presentation"
>
	<div class="modal" role="dialog" aria-modal="true" aria-labelledby={title ? 'modal-title' : undefined}>
		{#if title}
			<div class="modal-header">
				<h2 id="modal-title" class="modal-title">{title}</h2>
				<button class="close-button" onclick={onClose} aria-label="Close">
					<Icon name="x" size={20} />
				</button>
			</div>
		{:else}
			<button class="close-button absolute" onclick={onClose} aria-label="Close">
				<Icon name="x" size={20} />
			</button>
		{/if}

		<div class="modal-content">
			{@render children()}
		</div>
	</div>
</div>

<style>
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.7);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: var(--z-modal);
		padding: var(--space-4);
	}

	.modal {
		position: relative;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-xl);
		max-width: 600px;
		width: 100%;
		max-height: 90vh;
		overflow: hidden;
		display: flex;
		flex-direction: column;
	}

	.modal-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.modal-title {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
	}

	.close-button {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		background: transparent;
		border: none;
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		transition: all var(--transition-fast);
	}

	.close-button:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.close-button.absolute {
		position: absolute;
		top: var(--space-3);
		right: var(--space-3);
	}

	.modal-content {
		padding: var(--space-4);
		overflow: auto;
	}
</style>
