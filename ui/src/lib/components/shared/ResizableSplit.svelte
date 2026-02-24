<script lang="ts">
	/**
	 * ResizableSplit - A horizontal split panel with draggable divider.
	 *
	 * Features:
	 * - Drag divider to resize panels
	 * - Persists split ratio to localStorage
	 * - Respects min-width constraints
	 * - Keyboard accessible (arrow keys move divider)
	 */

	import type { Snippet } from 'svelte';

	interface Props {
		/** Unique identifier for persistence */
		id: string;
		/** Default split ratio (0-1, where 0.5 = 50/50) */
		defaultRatio?: number;
		/** Minimum width for left panel in pixels */
		minLeftWidth?: number;
		/** Minimum width for right panel in pixels */
		minRightWidth?: number;
		/** Left panel content */
		left: Snippet;
		/** Right panel content */
		right: Snippet;
		/** Left panel title */
		leftTitle?: string;
		/** Right panel title */
		rightTitle?: string;
		/** Optional left header actions */
		leftActions?: Snippet;
		/** Optional right header actions */
		rightActions?: Snippet;
	}

	let {
		id,
		defaultRatio = 0.5,
		minLeftWidth = 200,
		minRightWidth = 200,
		left,
		right,
		leftTitle,
		rightTitle,
		leftActions,
		rightActions
	}: Props = $props();

	let containerRef = $state<HTMLElement | null>(null);
	let ratio = $state(0.5);
	let isDragging = $state(false);
	let initialized = $state(false);

	// Initialize ratio from localStorage or defaultRatio
	$effect(() => {
		if (initialized) return;
		const stored = localStorage.getItem(`split-ratio-${id}`);
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
		localStorage.setItem(`split-ratio-${id}`, ratio.toString());
	});

	function handlePointerDown(e: PointerEvent) {
		isDragging = true;
		(e.target as HTMLElement).setPointerCapture(e.pointerId);
	}

	function handlePointerMove(e: PointerEvent) {
		if (!isDragging || !containerRef) return;

		const rect = containerRef.getBoundingClientRect();
		const containerWidth = rect.width;
		const dividerWidth = 8; // Width of the divider
		const availableWidth = containerWidth - dividerWidth;

		// Calculate new ratio based on mouse position
		const mouseX = e.clientX - rect.left;
		let newRatio = mouseX / containerWidth;

		// Clamp to respect min widths
		const minRatioLeft = minLeftWidth / availableWidth;
		const maxRatioLeft = 1 - (minRightWidth / availableWidth);

		newRatio = Math.max(minRatioLeft, Math.min(maxRatioLeft, newRatio));
		ratio = newRatio;
	}

	function handlePointerUp() {
		isDragging = false;
	}

	function handleKeyDown(e: KeyboardEvent) {
		const step = 0.02;
		if (e.key === 'ArrowLeft') {
			e.preventDefault();
			ratio = Math.max(0.1, ratio - step);
		} else if (e.key === 'ArrowRight') {
			e.preventDefault();
			ratio = Math.min(0.9, ratio + step);
		}
	}
</script>

<div
	class="resizable-split"
	class:dragging={isDragging}
	bind:this={containerRef}
>
	<div class="panel panel-left" style:flex-basis="{ratio * 100}%">
		{#if leftTitle}
			<header class="panel-header">
				<h3 class="panel-title">{leftTitle}</h3>
				{#if leftActions}
					<div class="header-actions">
						{@render leftActions()}
					</div>
				{/if}
			</header>
		{/if}
		<div class="panel-content">
			{@render left()}
		</div>
	</div>

	<button
		type="button"
		class="divider"
		aria-label="Resize panels, currently {Math.round(ratio * 100)}% left. Use left and right arrow keys to adjust."
		onpointerdown={handlePointerDown}
		onpointermove={handlePointerMove}
		onpointerup={handlePointerUp}
		onkeydown={handleKeyDown}
	>
		<span class="divider-handle" aria-hidden="true"></span>
	</button>

	<div class="panel panel-right" style:flex-basis="{(1 - ratio) * 100}%">
		{#if rightTitle}
			<header class="panel-header">
				<h3 class="panel-title">{rightTitle}</h3>
				{#if rightActions}
					<div class="header-actions">
						{@render rightActions()}
					</div>
				{/if}
			</header>
		{/if}
		<div class="panel-content">
			{@render right()}
		</div>
	</div>
</div>

<style>
	.resizable-split {
		display: flex;
		flex: 1;
		min-height: 0;
		gap: 0;
	}

	.resizable-split.dragging {
		cursor: col-resize;
		user-select: none;
	}

	.panel {
		display: flex;
		flex-direction: column;
		min-width: 0;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.panel-left {
		border-right: none;
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}

	.panel-right {
		border-left: none;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
	}

	.panel-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		min-height: 40px;
		flex-shrink: 0;
	}

	.panel-title {
		flex: 1;
		margin: 0;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.panel-content {
		flex: 1;
		overflow: auto;
		padding: var(--space-4);
	}

	.divider {
		width: 12px;
		margin: 0 var(--space-1);
		padding: 0;
		border: none;
		border-radius: var(--radius-sm);
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: col-resize;
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
		width: 4px;
		height: 32px;
		background: var(--color-text-muted);
		border-radius: 2px;
		opacity: 0.5;
		transition: opacity 0.15s ease;
	}

	.divider:hover .divider-handle,
	.resizable-split.dragging .divider-handle {
		opacity: 1;
		background: var(--color-accent);
	}

	/* Mobile: stack vertically, no divider */
	@media (max-width: 900px) {
		.resizable-split {
			flex-direction: column;
		}

		.panel {
			flex-basis: auto !important;
		}

		.panel-left {
			border-right: 1px solid var(--color-border);
			border-radius: var(--radius-md);
		}

		.panel-right {
			border-left: 1px solid var(--color-border);
			border-radius: var(--radius-md);
			margin-top: var(--space-4);
		}

		.divider {
			display: none;
		}
	}
</style>
