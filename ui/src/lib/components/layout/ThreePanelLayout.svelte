<script lang="ts">
	/**
	 * ThreePanelLayout - VS Code-style three-column layout.
	 *
	 * Left panel (Explorer): collapsible via Cmd+B, resizable 200–400px, default 260px.
	 * Center panel (Canvas): flex, always visible, minimum 400px.
	 * Right panel (Properties): collapsible via Cmd+J, resizable 240–480px, default 320px.
	 * Cmd+\ toggles both panels for focus mode.
	 *
	 * Panel widths and open state are persisted to localStorage using the `id` prop.
	 *
	 * Usage:
	 * ```svelte
	 * <ThreePanelLayout id="plan-detail" leftOpen={true} rightOpen={false}>
	 *   {#snippet leftPanel()}...{/snippet}
	 *   {#snippet centerPanel()}...{/snippet}
	 *   {#snippet rightPanel()}...{/snippet}
	 * </ThreePanelLayout>
	 * ```
	 */

	import { onMount } from 'svelte';
	import type { Snippet } from 'svelte';
	import ResizeHandle from './ResizeHandle.svelte';

	interface ThreePanelLayoutProps {
		/** Storage key prefix for localStorage persistence */
		id: string;
		/** Initial left panel visibility (overridden by localStorage on first render) */
		leftOpen?: boolean;
		/** Initial right panel visibility (overridden by localStorage on first render) */
		rightOpen?: boolean;
		/** Initial left panel width in pixels */
		leftWidth?: number;
		/** Initial right panel width in pixels */
		rightWidth?: number;
		/** Fired when left panel open state changes */
		onLeftToggle?: (open: boolean) => void;
		/** Fired when right panel open state changes */
		onRightToggle?: (open: boolean) => void;
		/** Left panel content */
		leftPanel: Snippet;
		/** Center panel content */
		centerPanel: Snippet;
		/** Right panel content */
		rightPanel: Snippet;
	}

	let {
		id,
		leftOpen = true,
		rightOpen = true,
		leftWidth = 260,
		rightWidth = 320,
		onLeftToggle,
		onRightToggle,
		leftPanel,
		centerPanel,
		rightPanel
	}: ThreePanelLayoutProps = $props();

	// Panel size constraints
	const LEFT_MIN = 200;
	const LEFT_MAX = 400;
	const RIGHT_MIN = 240;
	const RIGHT_MAX = 480;

	// Reactive panel state — seeded from props on mount, then overridden by localStorage if available.
	let leftPanelOpen = $state(true);
	let rightPanelOpen = $state(true);
	let leftPanelWidth = $state(260);
	let rightPanelWidth = $state(320);

	onMount(() => {
		// Apply prop defaults first
		leftPanelOpen = leftOpen;
		rightPanelOpen = rightOpen;
		leftPanelWidth = leftWidth;
		rightPanelWidth = rightWidth;

		// Then override with localStorage if persisted values exist
		const storedLeftOpen = localStorage.getItem(KEY_LEFT_OPEN);
		const storedRightOpen = localStorage.getItem(KEY_RIGHT_OPEN);
		const storedLeftWidth = localStorage.getItem(KEY_LEFT_WIDTH);
		const storedRightWidth = localStorage.getItem(KEY_RIGHT_WIDTH);

		if (storedLeftOpen !== null) leftPanelOpen = storedLeftOpen === 'true';
		if (storedRightOpen !== null) rightPanelOpen = storedRightOpen === 'true';
		if (storedLeftWidth !== null) {
			const w = parseInt(storedLeftWidth, 10);
			if (!isNaN(w) && w >= LEFT_MIN && w <= LEFT_MAX) leftPanelWidth = w;
		}
		if (storedRightWidth !== null) {
			const w = parseInt(storedRightWidth, 10);
			if (!isNaN(w) && w >= RIGHT_MIN && w <= RIGHT_MAX) rightPanelWidth = w;
		}
	});

	// Delta accumulated during an active drag — reset to 0 on drag end
	let leftDragDelta = $state(0);
	let rightDragDelta = $state(0);

	// Effective widths clamp prop + delta within constraints
	const effectiveLeftWidth = $derived(
		Math.min(Math.max(leftPanelWidth + leftDragDelta, LEFT_MIN), LEFT_MAX)
	);
	const effectiveRightWidth = $derived(
		Math.min(Math.max(rightPanelWidth + rightDragDelta, RIGHT_MIN), RIGHT_MAX)
	);

	// localStorage keys — derived so they react if `id` ever changes
	const KEY_LEFT_OPEN = $derived(`three-panel-${id}-left-open`);
	const KEY_RIGHT_OPEN = $derived(`three-panel-${id}-right-open`);
	const KEY_LEFT_WIDTH = $derived(`three-panel-${id}-left-width`);
	const KEY_RIGHT_WIDTH = $derived(`three-panel-${id}-right-width`);

	// (localStorage restore is handled in onMount above)

	// Persist open state changes
	$effect(() => {
		if (typeof localStorage === 'undefined') return;
		localStorage.setItem(KEY_LEFT_OPEN, String(leftPanelOpen));
	});

	$effect(() => {
		if (typeof localStorage === 'undefined') return;
		localStorage.setItem(KEY_RIGHT_OPEN, String(rightPanelOpen));
	});

	// Panel toggle functions
	function toggleLeft() {
		leftPanelOpen = !leftPanelOpen;
		onLeftToggle?.(leftPanelOpen);
	}

	function toggleRight() {
		rightPanelOpen = !rightPanelOpen;
		onRightToggle?.(rightPanelOpen);
	}

	// Resize callbacks for left handle
	function handleLeftResize(delta: number) {
		leftDragDelta += delta;
	}

	function handleLeftResizeEnd() {
		leftPanelWidth = effectiveLeftWidth;
		leftDragDelta = 0;
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem(KEY_LEFT_WIDTH, String(leftPanelWidth));
		}
	}

	// Resize callbacks for right handle
	function handleRightResize(delta: number) {
		rightDragDelta += delta;
	}

	function handleRightResizeEnd() {
		rightPanelWidth = effectiveRightWidth;
		rightDragDelta = 0;
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem(KEY_RIGHT_WIDTH, String(rightPanelWidth));
		}
	}

	// Keyboard shortcuts — skip when an input or textarea has focus
	function handleKeyDown(event: KeyboardEvent) {
		const target = event.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
			return;
		}

		const isMod = event.metaKey || event.ctrlKey;
		if (!isMod) return;

		if (event.key === 'b' && !event.shiftKey) {
			event.preventDefault();
			toggleLeft();
		} else if (event.key === 'j' && !event.shiftKey) {
			event.preventDefault();
			toggleRight();
		} else if (event.key === '\\') {
			event.preventDefault();
			// Focus mode: if either panel is open, close both; otherwise open both
			const anyOpen = leftPanelOpen || rightPanelOpen;
			leftPanelOpen = !anyOpen;
			rightPanelOpen = !anyOpen;
			onLeftToggle?.(leftPanelOpen);
			onRightToggle?.(rightPanelOpen);
		}
	}

	// CSS grid template derived from current state
	const gridTemplate = $derived.by(() => {
		const parts: string[] = [];

		if (leftPanelOpen) {
			parts.push(`${effectiveLeftWidth}px`); // left panel
			parts.push('auto'); // left resize handle
		}

		parts.push('auto'); // left toggle button
		parts.push('1fr'); // center always fills remaining space
		parts.push('auto'); // right toggle button

		if (rightPanelOpen) {
			parts.push('auto'); // right resize handle
			parts.push(`${effectiveRightWidth}px`); // right panel
		}

		return parts.join(' ');
	});
</script>

<svelte:window onkeydown={handleKeyDown} />

<div
	class="three-panel-layout"
	style="grid-template-columns: {gridTemplate};"
	data-testid="three-panel-layout"
>
	{#if leftPanelOpen}
		<aside class="panel panel-left" data-testid="panel-left">
			{@render leftPanel()}
		</aside>

		<ResizeHandle direction="left" onResize={handleLeftResize} onResizeEnd={handleLeftResizeEnd} />
	{/if}

	<button
		type="button"
		class="panel-toggle panel-toggle-left"
		onclick={toggleLeft}
		title={leftPanelOpen ? 'Collapse left panel (Cmd+B)' : 'Expand left panel (Cmd+B)'}
		aria-label={leftPanelOpen ? 'Collapse left panel' : 'Expand left panel'}
		data-testid="toggle-left"
	>
		<!-- Double angle: « when open (collapse), » when closed (expand) -->
		{leftPanelOpen ? '\u00AB' : '\u00BB'}
	</button>

	<main class="panel panel-center" data-testid="panel-center">
		{@render centerPanel()}
	</main>

	<button
		type="button"
		class="panel-toggle panel-toggle-right"
		onclick={toggleRight}
		title={rightPanelOpen ? 'Collapse right panel (Cmd+J)' : 'Expand right panel (Cmd+J)'}
		aria-label={rightPanelOpen ? 'Collapse right panel' : 'Expand right panel'}
		data-testid="toggle-right"
	>
		<!-- Mirror: » when open (collapse left), « when closed (expand left) -->
		{rightPanelOpen ? '\u00BB' : '\u00AB'}
	</button>

	{#if rightPanelOpen}
		<ResizeHandle
			direction="right"
			onResize={handleRightResize}
			onResizeEnd={handleRightResizeEnd}
		/>

		<aside class="panel panel-right" data-testid="panel-right">
			{@render rightPanel()}
		</aside>
	{/if}
</div>

<style>
	.three-panel-layout {
		display: grid;
		height: 100%;
		width: 100%;
		overflow: hidden;
		transition: grid-template-columns 200ms ease-out;
	}

	/* Respect user's reduced-motion preference */
	@media (prefers-reduced-motion: reduce) {
		.three-panel-layout {
			transition: none;
		}
	}

	/* --- Panels --- */

	.panel {
		overflow: hidden;
		display: flex;
		flex-direction: column;
		min-width: 0; /* allow grid children to shrink */
	}

	.panel-left {
		background: var(--color-bg-secondary);
		border-right: 1px solid var(--color-border);
	}

	.panel-center {
		background: var(--color-bg-primary);
		/* Prevent center from collapsing below 400px; grid enforces this via 1fr */
		min-width: 400px;
		overflow: auto;
	}

	.panel-right {
		background: var(--color-bg-secondary);
		border-left: 1px solid var(--color-border);
		overflow: auto;
	}

	/* --- Toggle buttons --- */

	.panel-toggle {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 18px;
		height: 100%;
		padding: 0;
		border: none;
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		cursor: pointer;
		font-size: var(--font-size-xs);
		line-height: 1;
		transition: background-color 150ms ease, color 150ms ease;
		flex-shrink: 0;
	}

	.panel-toggle:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.panel-toggle:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}

	/* Mobile: collapse side panels, stack vertically */
	@media (max-width: 768px) {
		.three-panel-layout {
			grid-template-columns: 1fr !important;
			grid-template-rows: auto;
			transition: none;
		}

		.panel-left,
		.panel-right,
		.panel-toggle {
			display: none;
		}

		.panel-center {
			min-width: 0;
		}
	}
</style>
