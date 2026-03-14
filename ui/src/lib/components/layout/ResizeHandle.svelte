<script lang="ts">
	/**
	 * ResizeHandle - Draggable divider between panels.
	 *
	 * Supports mouse drag, touch drag, and keyboard arrow keys.
	 * Reports delta pixels via onResize callback; fires onResizeEnd when drag completes.
	 * Keyboard: ArrowLeft/ArrowRight move 10px, Shift moves 50px.
	 */

	interface ResizeHandleProps {
		/** Which panel this handle is adjacent to — affects delta sign convention */
		direction: 'left' | 'right';
		/** Fired continuously during drag with pixel delta */
		onResize?: (delta: number) => void;
		/** Fired once when drag ends (commit the accumulated delta) */
		onResizeEnd?: () => void;
		/** Disable all interactions */
		disabled?: boolean;
	}

	let { direction, onResize, onResizeEnd, disabled = false }: ResizeHandleProps = $props();

	let isDragging = $state(false);
	let startX = $state(0);

	// Mouse drag

	function handleMouseDown(event: MouseEvent) {
		if (disabled) return;
		event.preventDefault();
		isDragging = true;
		startX = event.clientX;

		document.addEventListener('mousemove', handleMouseMove);
		document.addEventListener('mouseup', handleMouseUp);
	}

	function handleMouseMove(event: MouseEvent) {
		if (!isDragging) return;
		const delta = event.clientX - startX;
		startX = event.clientX;
		// Left handle: positive delta grows the left panel.
		// Right handle: negative delta grows the right panel.
		onResize?.(direction === 'left' ? delta : -delta);
	}

	function handleMouseUp() {
		isDragging = false;
		document.removeEventListener('mousemove', handleMouseMove);
		document.removeEventListener('mouseup', handleMouseUp);
		onResizeEnd?.();
	}

	// Touch drag

	function handleTouchStart(event: TouchEvent) {
		if (disabled) return;
		event.preventDefault();
		isDragging = true;
		startX = event.touches[0].clientX;

		document.addEventListener('touchmove', handleTouchMove, { passive: false });
		document.addEventListener('touchend', handleTouchEnd);
	}

	function handleTouchMove(event: TouchEvent) {
		if (!isDragging) return;
		event.preventDefault();
		const delta = event.touches[0].clientX - startX;
		startX = event.touches[0].clientX;
		onResize?.(direction === 'left' ? delta : -delta);
	}

	function handleTouchEnd() {
		isDragging = false;
		document.removeEventListener('touchmove', handleTouchMove);
		document.removeEventListener('touchend', handleTouchEnd);
		onResizeEnd?.();
	}

	// Keyboard accessibility

	function handleKeyDown(event: KeyboardEvent) {
		if (disabled) return;

		if (event.key === 'Escape') {
			(event.currentTarget as HTMLElement)?.blur();
			return;
		}

		const step = event.shiftKey ? 50 : 10;
		let delta = 0;

		switch (event.key) {
			case 'ArrowLeft':
				delta = direction === 'left' ? -step : step;
				break;
			case 'ArrowRight':
				delta = direction === 'left' ? step : -step;
				break;
			default:
				return;
		}

		event.preventDefault();
		onResize?.(delta);
		onResizeEnd?.();
	}

	const ariaLabel = $derived(direction === 'left' ? 'Resize left panel' : 'Resize right panel');
</script>

<button
	type="button"
	class="resize-handle"
	class:dragging={isDragging}
	aria-label={ariaLabel}
	{disabled}
	onmousedown={handleMouseDown}
	ontouchstart={handleTouchStart}
	onkeydown={handleKeyDown}
	data-testid="resize-handle-{direction}"
>
	<span class="handle-line" aria-hidden="true"></span>
</button>

<style>
	.resize-handle {
		position: relative;
		width: 4px;
		cursor: col-resize;
		background: transparent;
		border: none;
		padding: 0;
		margin: 0;
		appearance: none;
		flex-shrink: 0;
		z-index: 10;
		transition: background-color 150ms ease, width 150ms ease;
	}

	/* Wider invisible hit area for easier grabbing */
	.resize-handle::before {
		content: '';
		position: absolute;
		top: 0;
		bottom: 0;
		left: -4px;
		right: -4px;
	}

	.resize-handle:hover,
	.resize-handle:focus-visible {
		width: 6px;
		background: var(--color-accent);
	}

	.resize-handle.dragging {
		background: var(--color-accent-hover);
	}

	.resize-handle:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}

	.resize-handle:disabled {
		cursor: default;
		pointer-events: none;
		opacity: 0.5;
	}

	/* Visual grip indicator */
	.handle-line {
		position: absolute;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		width: 2px;
		height: 24px;
		background: var(--color-border);
		border-radius: 1px;
		opacity: 0;
		transition: opacity 150ms ease;
	}

	.resize-handle:hover .handle-line,
	.resize-handle:focus-visible .handle-line,
	.resize-handle.dragging .handle-line {
		opacity: 1;
		background: var(--color-text-primary);
	}
</style>
