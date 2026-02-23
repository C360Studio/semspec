<script lang="ts">
	import { fly } from 'svelte/transition';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import { settingsStore } from '$lib/stores/settings.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';

	let drawerElement = $state<HTMLDivElement | null>(null);

	// Cache focusable elements for efficient focus trap
	let focusableElements = $state<HTMLElement[]>([]);
	let firstFocusable = $derived(focusableElements[0] ?? null);
	let lastFocusable = $derived(focusableElements[focusableElements.length - 1] ?? null);

	/**
	 * Handle Escape key to close drawer and Tab for focus trap.
	 */
	function handleKeydown(e: KeyboardEvent): void {
		// Only handle events when drawer is open
		if (!chatDrawerStore.isOpen) return;

		if (e.key === 'Escape') {
			chatDrawerStore.close();
		}

		// Focus trap: cycle focus within drawer when Tab is pressed
		if (e.key === 'Tab' && focusableElements.length > 0) {
			if (e.shiftKey) {
				// Shift+Tab: wrap from first to last
				if (document.activeElement === firstFocusable) {
					e.preventDefault();
					lastFocusable?.focus();
				}
			} else {
				// Tab: wrap from last to first
				if (document.activeElement === lastFocusable) {
					e.preventDefault();
					firstFocusable?.focus();
				}
			}
		}
	}

	/**
	 * Handle backdrop click to close drawer.
	 */
	function handleBackdropClick(e: MouseEvent): void {
		// Only close if clicking the backdrop itself, not the drawer
		if (e.target === e.currentTarget) {
			chatDrawerStore.close();
		}
	}

	/**
	 * Cache focusable elements and focus first input when drawer opens.
	 */
	$effect(() => {
		if (chatDrawerStore.isOpen && drawerElement) {
			// Wait for DOM to update, then cache focusable elements
			requestAnimationFrame(() => {
				const elements = drawerElement?.querySelectorAll<HTMLElement>(
					'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
				);
				focusableElements = elements ? Array.from(elements) : [];

				// Focus first input
				const firstInput = drawerElement?.querySelector<HTMLElement>('textarea, input');
				firstInput?.focus();
			});
		} else {
			// Clear cache when drawer closes
			focusableElements = [];
		}
	});
</script>

<svelte:window onkeydown={handleKeydown} />

{#if chatDrawerStore.isOpen}
	<div
		class="chat-drawer-backdrop"
		onclick={handleBackdropClick}
		role="presentation"
		transition:fly={{
			x: 0,
			opacity: 0,
			duration: settingsStore.reducedMotion ? 0 : 200
		}}
	>
		<div
			bind:this={drawerElement}
			class="chat-drawer"
			role="dialog"
			aria-modal="true"
			aria-label={chatDrawerStore.contextTitle}
			transition:fly={{
				x: 400,
				duration: settingsStore.reducedMotion ? 0 : 200
			}}
		>
			<div class="drawer-header">
				<h2 class="drawer-title">{chatDrawerStore.contextTitle}</h2>
				<button
					class="close-button"
					onclick={() => chatDrawerStore.close()}
					aria-label="Close chat drawer"
				>
					<Icon name="x" size={20} />
				</button>
			</div>

			<div class="drawer-content">
				<ChatPanel
					title={chatDrawerStore.contextTitle}
					planSlug={chatDrawerStore.context.planSlug}
				/>
			</div>
		</div>
	</div>
{/if}

<style>
	.chat-drawer-backdrop {
		position: fixed;
		top: 0;
		left: 0;
		right: 0;
		bottom: 0;
		background: rgba(0, 0, 0, 0.5);
		backdrop-filter: blur(2px);
		z-index: 1000;
		display: flex;
		justify-content: flex-end;
	}

	.chat-drawer {
		width: var(--chat-drawer-width, 400px);
		height: 100%;
		background: var(--color-bg-primary);
		box-shadow: -4px 0 16px rgba(0, 0, 0, 0.2);
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.drawer-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.drawer-title {
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
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
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.close-button:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.close-button:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.drawer-content {
		flex: 1;
		overflow: hidden;
		padding: var(--space-4);
	}

	/* Mobile: full-screen takeover */
	@media (max-width: 900px) {
		.chat-drawer {
			width: 100vw;
			height: 100vh;
		}
	}

	/* Respect reduced motion preference */
	:global(.reduced-motion) .chat-drawer-backdrop,
	:global(.reduced-motion) .chat-drawer {
		transition: none !important;
	}
</style>
