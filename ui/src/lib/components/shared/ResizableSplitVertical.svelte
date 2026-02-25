<script lang="ts">
	/**
	 * ResizableSplitVertical - A vertical split panel with draggable divider.
	 *
	 * Features:
	 * - Drag divider to resize panels (top/bottom)
	 * - Persists split ratio to localStorage
	 * - Respects min-height constraints
	 * - Keyboard accessible (arrow keys move divider)
	 */

	import type { Snippet } from 'svelte';

	interface Props {
		/** Unique identifier for persistence */
		id: string;
		/** Default split ratio (0-1, where 0.6 = 60% top, 40% bottom) */
		defaultRatio?: number;
		/** Minimum height for top panel in pixels */
		minTopHeight?: number;
		/** Minimum height for bottom panel in pixels */
		minBottomHeight?: number;
		/** Top panel content */
		top: Snippet;
		/** Bottom panel content */
		bottom: Snippet;
		/** Top panel title */
		topTitle?: string;
		/** Bottom panel title */
		bottomTitle?: string;
	}

	let {
		id,
		defaultRatio = 0.6,
		minTopHeight = 150,
		minBottomHeight = 150,
		top,
		bottom,
		topTitle,
		bottomTitle
	}: Props = $props();

	let containerRef = $state<HTMLElement | null>(null);
	let ratio = $state(0.6);
	let isDragging = $state(false);
	let initialized = $state(false);

	// Initialize ratio from localStorage or defaultRatio
	$effect(() => {
		if (initialized) return;
		const stored = localStorage.getItem(`vsplit-ratio-${id}`);
		if (stored) {
			const parsed = parseFloat(stored);
			if (!isNaN(parsed) && parsed >= 0.1 && parsed <= 0.9) {
				ratio = parsed;
				initialized = true;
				return;
			}
		}
		ratio = defaultRatio;
		initialized = true;
	});

	// Save ratio when it changes
	$effect(() => {
		localStorage.setItem(`vsplit-ratio-${id}`, ratio.toString());
	});

	function handlePointerDown(e: PointerEvent) {
		isDragging = true;
		(e.target as HTMLElement).setPointerCapture(e.pointerId);
	}

	function handlePointerMove(e: PointerEvent) {
		if (!isDragging || !containerRef) return;

		const rect = containerRef.getBoundingClientRect();
		const containerHeight = rect.height;
		const dividerHeight = 8;
		const availableHeight = containerHeight - dividerHeight;

		// Calculate new ratio based on mouse position
		const mouseY = e.clientY - rect.top;
		let newRatio = mouseY / containerHeight;

		// Clamp to respect min heights
		const minRatioTop = minTopHeight / availableHeight;
		const maxRatioTop = 1 - (minBottomHeight / availableHeight);

		newRatio = Math.max(minRatioTop, Math.min(maxRatioTop, newRatio));
		ratio = newRatio;
	}

	function handlePointerUp() {
		isDragging = false;
	}

	function handleKeyDown(e: KeyboardEvent) {
		const step = 0.02;
		if (e.key === 'ArrowUp') {
			e.preventDefault();
			ratio = Math.max(0.1, ratio - step);
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			ratio = Math.min(0.9, ratio + step);
		}
	}
</script>

<div
	class="resizable-split-vertical"
	class:dragging={isDragging}
	bind:this={containerRef}
>
	<div class="panel panel-top" style:flex-basis="{ratio * 100}%">
		{#if topTitle}
			<header class="panel-header">
				<h3 class="panel-title">{topTitle}</h3>
			</header>
		{/if}
		<div class="panel-content">
			{@render top()}
		</div>
	</div>

	<button
		type="button"
		class="divider"
		aria-label="Resize panels, currently {Math.round(ratio * 100)}% top. Use up and down arrow keys to adjust."
		onpointerdown={handlePointerDown}
		onpointermove={handlePointerMove}
		onpointerup={handlePointerUp}
		onkeydown={handleKeyDown}
	>
		<span class="divider-handle" aria-hidden="true"></span>
	</button>

	<div class="panel panel-bottom" style:flex-basis="{(1 - ratio) * 100}%">
		{#if bottomTitle}
			<header class="panel-header">
				<h3 class="panel-title">{bottomTitle}</h3>
			</header>
		{/if}
		<div class="panel-content">
			{@render bottom()}
		</div>
	</div>
</div>

<style>
	.resizable-split-vertical {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
		gap: 0;
	}

	.resizable-split-vertical.dragging {
		cursor: row-resize;
		user-select: none;
	}

	.panel {
		display: flex;
		flex-direction: column;
		min-height: 0;
		overflow: hidden;
	}

	.panel-top {
		border-bottom: none;
	}

	.panel-bottom {
		border-top: none;
	}

	.panel-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		min-height: 36px;
		flex-shrink: 0;
	}

	.panel-title {
		flex: 1;
		margin: 0;
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.panel-content {
		flex: 1;
		overflow: auto;
		min-height: 0;
	}

	.divider {
		height: 8px;
		padding: 0;
		border: none;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: row-resize;
		background: var(--color-bg-tertiary);
		transition: background-color 0.15s ease;
		flex-shrink: 0;
	}

	.divider:hover,
	.divider:focus-visible {
		background: var(--color-accent-muted);
	}

	.divider:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}

	.divider-handle {
		width: 32px;
		height: 4px;
		background: var(--color-text-muted);
		border-radius: 2px;
		opacity: 0.5;
		transition: opacity 0.15s ease;
	}

	.divider:hover .divider-handle,
	.resizable-split-vertical.dragging .divider-handle {
		opacity: 1;
		background: var(--color-accent);
	}
</style>
