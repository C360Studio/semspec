<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import type { ChatDrawerContext } from '$lib/stores/chatDrawer.svelte';

	interface Props {
		context: ChatDrawerContext;
		variant?: 'icon' | 'button';
		class?: string;
	}

	let { context, variant = 'button', class: className = '' }: Props = $props();

	function handleClick(): void {
		chatDrawerStore.open(context);
	}

	const ariaLabel = $derived.by(() => {
		switch (context.type) {
			case 'plan':
				return `Open chat for plan ${context.planSlug}`;
			case 'task':
				return `Open chat for task ${context.taskId}`;
			case 'question':
				return `Open chat for question ${context.questionId}`;
			default:
				return 'Open chat';
		}
	});
</script>

{#if variant === 'icon'}
	<button
		class="trigger-icon {className}"
		onclick={handleClick}
		aria-label={ariaLabel}
		title={ariaLabel}
	>
		<Icon name="message-square" size={20} />
	</button>
{:else}
	<button class="trigger-button {className}" onclick={handleClick} aria-label={ariaLabel}>
		<Icon name="message-square" size={16} />
		<span>Ask</span>
	</button>
{/if}

<style>
	.trigger-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.trigger-icon:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.trigger-icon:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.trigger-button {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		color: white;
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.trigger-button:hover {
		background: var(--color-accent-hover);
	}

	.trigger-button:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.trigger-button span {
		line-height: 1;
	}
</style>
