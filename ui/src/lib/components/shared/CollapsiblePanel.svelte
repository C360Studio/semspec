<script lang="ts">
	/**
	 * CollapsiblePanel - A panel that can be collapsed/expanded with state persistence.
	 *
	 * Features:
	 * - Collapse/expand toggle in header
	 * - State persisted to localStorage by panel ID
	 * - Smooth CSS transitions
	 * - Collapsed state shows thin strip with title + expand button
	 * - Works with flex layouts (remaining panels expand when one collapses)
	 */

	import type { Snippet } from 'svelte';
	import { panelState } from '$lib/stores/panelState.svelte';

	interface Props {
		/** Unique identifier for the panel (used for state persistence) */
		id: string;
		/** Title displayed in the header */
		title: string;
		/** Whether the panel starts open (default: true) */
		defaultOpen?: boolean;
		/** Base width when expanded (CSS value, e.g., '300px', '25%') */
		width?: string;
		/** Minimum width when expanded */
		minWidth?: string;
		/** Whether this panel should flex to fill available space */
		flex?: boolean;
		/** Content to render inside the panel */
		children: Snippet;
		/** Optional header actions (rendered next to collapse button) */
		headerActions?: Snippet;
	}

	let {
		id,
		title,
		defaultOpen = true,
		width,
		minWidth,
		flex = false,
		children,
		headerActions
	}: Props = $props();

	// Register panel and get initial state
	const initialOpen = panelState.register({ id, defaultOpen });

	// Track open state reactively
	let isOpen = $derived(panelState.isOpen(id));

	function toggle() {
		panelState.toggle(id);
	}
</script>

<div
	class="collapsible-panel"
	class:collapsed={!isOpen}
	class:flex
	style:width={isOpen ? width : undefined}
	style:min-width={isOpen ? minWidth : undefined}
	data-panel-id={id}
>
	<header class="panel-header">
		<button
			type="button"
			class="collapse-toggle"
			onclick={toggle}
			aria-expanded={isOpen}
			aria-controls="panel-content-{id}"
			title={isOpen ? `Collapse ${title}` : `Expand ${title}`}
		>
			<span class="toggle-icon" class:rotated={!isOpen}>
				<svg
					width="12"
					height="12"
					viewBox="0 0 12 12"
					fill="none"
					xmlns="http://www.w3.org/2000/svg"
				>
					<path
						d="M3 4.5L6 7.5L9 4.5"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				</svg>
			</span>
		</button>
		<h3 class="panel-title">{title}</h3>
		{#if headerActions && isOpen}
			<div class="header-actions">
				{@render headerActions()}
			</div>
		{/if}
	</header>

	{#if isOpen}
		<div class="panel-content" id="panel-content-{id}">
			{@render children()}
		</div>
	{/if}
</div>

<style>
	.collapsible-panel {
		display: flex;
		flex-direction: column;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		overflow: hidden;
		transition:
			width 0.2s ease,
			min-width 0.2s ease,
			flex 0.2s ease;
	}

	.collapsible-panel.flex {
		flex: 1;
	}

	.collapsible-panel.collapsed {
		width: 40px !important;
		min-width: 40px !important;
		flex: 0 0 40px !important;
	}

	.collapsible-panel.collapsed .panel-header {
		flex-direction: column;
		padding: var(--space-2);
		gap: var(--space-2);
	}

	.collapsible-panel.collapsed .panel-title {
		writing-mode: vertical-rl;
		text-orientation: mixed;
		white-space: nowrap;
		transform: rotate(180deg);
		font-size: var(--font-size-xs);
	}

	.panel-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		min-height: 40px;
	}

	.collapse-toggle {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		padding: 0;
		border: none;
		background: transparent;
		color: var(--color-text-secondary);
		cursor: pointer;
		border-radius: var(--radius-sm);
		transition: background-color 0.15s ease;
	}

	.collapse-toggle:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}

	.collapse-toggle:focus-visible {
		outline: 2px solid var(--color-primary);
		outline-offset: 2px;
	}

	.toggle-icon {
		display: flex;
		transition: transform 0.2s ease;
	}

	.toggle-icon.rotated {
		transform: rotate(-90deg);
	}

	.panel-title {
		flex: 1;
		margin: 0;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.panel-content {
		flex: 1;
		overflow: auto;
	}

	/* When collapsed, hide the title on very narrow panels */
	@media (max-width: 768px) {
		.collapsible-panel.collapsed .panel-title {
			display: none;
		}
	}
</style>
