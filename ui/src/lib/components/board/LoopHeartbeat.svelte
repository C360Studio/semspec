<script lang="ts">
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { ActiveLoop } from '$lib/types/plan';

	interface Props {
		loops: ActiveLoop[];
		/**
		 * Idle threshold in seconds — beyond this the badge gets a warning
		 * treatment. Tuned to catch "model is thinking" (normal) vs "something
		 * is wrong" (attention). 90s is generous for local Ollama on 35b but
		 * short enough to surface a real hang quickly.
		 */
		staleAfterSec?: number;
	}

	let { loops, staleAfterSec = 90 }: Props = $props();

	// Re-render on a tick so idle seconds update in real time without depending
	// on new SSE events. The interval runs only while this component is
	// mounted, so it auto-unsubscribes when the plan card unmounts.
	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => {
			now = Date.now();
		}, 1000);
		return () => clearInterval(id);
	});

	// Collect per-loop heartbeats and compute the oldest-idle so we can pick
	// the plan-level rollup color. A non-stale loop keeps the card calm; any
	// stale loop tints the row so watchful humans see "one of the six is
	// quiet for a while" at a glance.
	const heartbeats = $derived.by(() => {
		const _ = now; // take a dep so $derived re-computes every tick
		void _;
		return loops.map((loop) => {
			const lastSeen = activityStore.loopLastSeen.get(loop.loop_id);
			const idleMs = lastSeen !== undefined ? Date.now() - lastSeen : null;
			return { loop, idleMs };
		});
	});

	const oldestIdleMs = $derived.by(() => {
		let max = 0;
		for (const { idleMs } of heartbeats) {
			if (idleMs !== null && idleMs > max) max = idleMs;
		}
		return max;
	});

	const anyStale = $derived(oldestIdleMs > staleAfterSec * 1000);

	function idleLabel(ms: number | null): string {
		if (ms === null) return 'starting';
		const s = Math.floor(ms / 1000);
		if (s < 1) return 'live';
		if (s < 60) return `${s}s ago`;
		const m = Math.floor(s / 60);
		return m < 60 ? `${m}m ago` : `${Math.floor(m / 60)}h ago`;
	}
</script>

{#if loops.length > 0}
	<div class="heartbeat" class:stale={anyStale} data-testid="plan-card-heartbeat">
		{#if loops.length === 1}
			{@const hb = heartbeats[0]}
			<span class="dot" class:pulse={!anyStale} aria-hidden="true"></span>
			<span class="role">{hb.loop.role || 'agent'}</span>
			{#if hb.loop.iterations !== undefined && hb.loop.max_iterations}
				<span class="counter">turn {hb.loop.iterations}/{hb.loop.max_iterations}</span>
			{/if}
			<span class="age" title="Time since last agent tick">· {idleLabel(hb.idleMs)}</span>
		{:else}
			<span class="dot" class:pulse={!anyStale} aria-hidden="true"></span>
			<span class="role">{loops.length} loops running</span>
			<span class="age" title="Oldest idle loop">· oldest {idleLabel(oldestIdleMs || null)}</span>
		{/if}
	</div>
{/if}

<style>
	.heartbeat {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.heartbeat.stale {
		color: var(--color-warning);
		font-weight: var(--font-weight-semibold);
	}

	.dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--color-accent);
		flex-shrink: 0;
	}

	.heartbeat.stale .dot {
		background: var(--color-warning);
	}

	.dot.pulse {
		animation: heartbeat-pulse 1.8s ease-in-out infinite;
	}

	.role {
		font-weight: var(--font-weight-medium);
	}

	.counter {
		font-family: var(--font-family-mono);
	}

	.age {
		font-family: var(--font-family-mono);
	}

	@keyframes heartbeat-pulse {
		0%,
		100% {
			opacity: 1;
			transform: scale(1);
		}
		50% {
			opacity: 0.5;
			transform: scale(1.3);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.dot.pulse {
			animation: none;
		}
	}
</style>
