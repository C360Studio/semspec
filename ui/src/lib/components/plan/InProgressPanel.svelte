<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';

	interface Props {
		title: string;
		detail?: string;
		phase?: string;
		phaseState?: string;
		/** ISO timestamp marking when this stage started — used to render an
		 * elapsed-time ticker so the user can tell "still moving" from "wedged"
		 * during long LLM phases. Null/undefined hides the ticker. */
		startedAt?: string | null;
	}

	let { title, detail, phase = 'planning', phaseState = 'active', startedAt = null }: Props = $props();

	const iconName = $derived.by(() => {
		if (phaseState === 'waiting') return 'pause';
		if (phaseState === 'failed' || phaseState === 'error') return 'alert-triangle';
		if (phaseState === 'complete') return 'check-circle';
		if (phase === 'execution') return 'play';
		return 'loader';
	});
	const spins = $derived(iconName === 'loader');

	// Live-updating elapsed time. The $effect is purely side-effectful
	// (registers and clears an interval) — the state assignment inside is
	// driven by the timer, not by reactive state cascading, so this is the
	// legitimate $effect shape per [[svelte5-effect-for-side-effects-only]].
	let elapsedMs = $state(0);
	$effect(() => {
		if (!startedAt) {
			elapsedMs = 0;
			return;
		}
		const start = new Date(startedAt).getTime();
		if (!Number.isFinite(start)) {
			elapsedMs = 0;
			return;
		}
		elapsedMs = Date.now() - start;
		const interval = setInterval(() => {
			elapsedMs = Date.now() - start;
		}, 1000);
		return () => clearInterval(interval);
	});

	function formatElapsed(ms: number): string {
		if (ms < 0 || !Number.isFinite(ms)) return '';
		const totalSec = Math.floor(ms / 1000);
		if (totalSec < 60) return `${totalSec}s`;
		const m = Math.floor(totalSec / 60);
		const s = totalSec % 60;
		return `${m}m ${s.toString().padStart(2, '0')}s`;
	}
</script>

<section class="in-progress-panel" data-phase={phase} data-state={phaseState} role="status" aria-live="polite">
	<div class="spinner-wrap" aria-hidden="true">
		<Icon name={iconName} size={28} class={spins ? 'spin' : ''} />
	</div>
	<div class="message">
		<h3 class="title">{title}</h3>
		{#if detail}<p class="detail">{detail}</p>{/if}
	</div>
	{#if startedAt}
		<div class="elapsed" aria-label="Elapsed time">
			<Icon name="clock" size={14} />
			<span>{formatElapsed(elapsedMs)}</span>
		</div>
	{/if}
</section>

<style>
	.in-progress-panel {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		padding: var(--space-4) var(--space-5);
		background: linear-gradient(
			135deg,
			var(--color-bg-elevated) 0%,
			var(--color-bg-secondary) 100%
		);
		border: 1px solid var(--color-accent);
		border-radius: var(--radius-lg);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 18%, transparent);
		/* Sticky so the panel stays visible as the user scrolls through the
		 * plan body (often sparse during drafting). The nearest scrolling
		 * ancestor is `.plan-content` in the route's `+page.svelte` which has
		 * `overflow-y: auto`. z-index lifts the panel above sibling content
		 * (PlanDetail's scope blocks, ExecutionTimeline cards) so the
		 * accent border doesn't get covered when content scrolls underneath. */
		position: sticky;
		top: 0;
		z-index: 2;
	}

	.spinner-wrap {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 44px;
		height: 44px;
		color: var(--color-accent);
		border-radius: 50%;
		background: color-mix(in srgb, var(--color-accent) 14%, transparent);
		animation: pulse 2s ease-in-out infinite;
		flex-shrink: 0;
	}

	.spinner-wrap :global(svg.spin) {
		animation: spin 1.6s linear infinite;
	}

	.message {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.title {
		margin: 0;
		font-size: var(--font-size-lg);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.detail {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
	}

	.elapsed {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		font-size: var(--font-size-sm);
		font-variant-numeric: tabular-nums;
		color: var(--color-text-secondary);
		flex-shrink: 0;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}

	@keyframes pulse {
		0%, 100% {
			box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-accent) 30%, transparent);
		}
		50% {
			box-shadow: 0 0 0 8px color-mix(in srgb, var(--color-accent) 0%, transparent);
		}
	}
</style>
