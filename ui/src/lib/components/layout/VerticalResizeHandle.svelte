<script lang="ts">
	/**
	 * VerticalResizeHandle - Full-width horizontal drag handle for Y-axis resizing.
	 *
	 * Reports resize deltas via the onResize callback. The parent is responsible
	 * for clamping the resulting height to acceptable min/max bounds.
	 */

	interface Props {
		onResize: (delta: number) => void;
		valueNow?: number;
		valueMin?: number;
		valueMax?: number;
	}

	let { onResize, valueNow = 0, valueMin = 150, valueMax = 800 }: Props = $props();

	let isDragging = $state(false);
	let lastY = $state(0);

	function handleMouseDown(event: MouseEvent): void {
		isDragging = true;
		lastY = event.clientY;
		event.preventDefault();
	}

	function handleTouchStart(event: TouchEvent): void {
		if (event.touches.length !== 1) return;
		isDragging = true;
		lastY = event.touches[0].clientY;
	}

	function handleMouseMove(event: MouseEvent): void {
		if (!isDragging) return;
		const delta = lastY - event.clientY;
		lastY = event.clientY;
		onResize(delta);
	}

	function handleTouchMove(event: TouchEvent): void {
		if (!isDragging || event.touches.length !== 1) return;
		const delta = lastY - event.touches[0].clientY;
		lastY = event.touches[0].clientY;
		onResize(delta);
	}

	function stopDragging(): void {
		isDragging = false;
	}

	function handleKeyDown(event: KeyboardEvent): void {
		if (event.key === 'ArrowUp') {
			event.preventDefault();
			onResize(10);
		} else if (event.key === 'ArrowDown') {
			event.preventDefault();
			onResize(-10);
		}
	}
</script>

<svelte:window
	onmousemove={handleMouseMove}
	onmouseup={stopDragging}
	ontouchmove={handleTouchMove}
	ontouchend={stopDragging}
/>

<div
	class="resize-handle"
	class:dragging={isDragging}
	role="slider"
	aria-orientation="horizontal"
	aria-label="Resize chat panel"
	aria-valuenow={valueNow}
	aria-valuemin={valueMin}
	aria-valuemax={valueMax}
	tabindex="0"
	onmousedown={handleMouseDown}
	ontouchstart={handleTouchStart}
	onkeydown={handleKeyDown}
></div>

<style>
	.resize-handle {
		width: 100%;
		height: 6px;
		background: var(--color-bg-secondary);
		border-top: 1px solid var(--color-border);
		cursor: ns-resize;
		flex-shrink: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background-color var(--transition-fast);
		user-select: none;
		touch-action: none;
	}

	.resize-handle::after {
		content: '';
		display: block;
		width: 32px;
		height: 3px;
		border-radius: 2px;
		background: var(--color-border);
		transition: background-color var(--transition-fast);
	}

	.resize-handle:hover,
	.resize-handle.dragging {
		background: var(--color-bg-tertiary);
	}

	.resize-handle:hover::after,
	.resize-handle.dragging::after {
		background: var(--color-accent);
	}

	.resize-handle:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}
</style>
