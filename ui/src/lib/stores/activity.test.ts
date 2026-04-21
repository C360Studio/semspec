/**
 * Tests for ActivityStore.loopLastSeen — the heartbeat timeline that powers
 * LoopHeartbeat.svelte / PlanCard liveness. These are the regressions we care
 * about:
 *   - loop_updated / loop_created tick the map
 *   - loop_deleted evicts cleanly (so idle-ms doesn't lie about a finished loop)
 *   - the Map reference is replaced on every mutation (Svelte 5 Map reactivity
 *     requires a new reference; in-place .set() would silently break PlanCard)
 *   - idleMsForLoop returns null for unknown loops, positive for known
 *
 * The store addEvent hook is private in prod to keep the SSE surface narrow;
 * we reach in via `as any` rather than promoting it to public — keeps the
 * production API clean and forces this test to be obvious about poking
 * internals.
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { activityStore } from '$lib/stores/activity.svelte';
import type { ActivityEvent } from '$lib/types';

function tick(event: Partial<ActivityEvent> & { type: string; loop_id?: string }): void {
	const full: ActivityEvent = {
		timestamp: new Date().toISOString(),
		loop_id: event.loop_id ?? '',
		...event
	} as ActivityEvent;
	// addEvent is the single entrypoint for both SSE and mock streams; calling
	// it directly exercises the same code path the server would.
	(activityStore as unknown as { addEvent: (e: ActivityEvent) => void }).addEvent(full);
}

describe('ActivityStore — loopLastSeen', () => {
	beforeEach(() => {
		activityStore.clear();
		// Reset the map by draining whatever previous tests left behind.
		const keys = Array.from(activityStore.loopLastSeen.keys());
		for (const id of keys) {
			tick({ type: 'loop_deleted', loop_id: id });
		}
	});

	it('records a timestamp on loop_created', () => {
		const before = Date.now();
		tick({ type: 'loop_created', loop_id: 'loop-a' });

		const seen = activityStore.loopLastSeen.get('loop-a');
		expect(seen).toBeDefined();
		expect(seen!).toBeGreaterThanOrEqual(before);
	});

	it('refreshes the timestamp on loop_updated', async () => {
		tick({ type: 'loop_created', loop_id: 'loop-b' });
		const first = activityStore.loopLastSeen.get('loop-b')!;

		await new Promise((r) => setTimeout(r, 5));
		tick({ type: 'loop_updated', loop_id: 'loop-b' });
		const second = activityStore.loopLastSeen.get('loop-b')!;

		expect(second).toBeGreaterThan(first);
	});

	it('evicts the entry on loop_deleted', () => {
		tick({ type: 'loop_created', loop_id: 'loop-c' });
		expect(activityStore.loopLastSeen.has('loop-c')).toBe(true);

		tick({ type: 'loop_deleted', loop_id: 'loop-c' });
		expect(activityStore.loopLastSeen.has('loop-c')).toBe(false);
	});

	it('tracks multiple loops independently', () => {
		tick({ type: 'loop_created', loop_id: 'loop-d' });
		tick({ type: 'loop_created', loop_id: 'loop-e' });

		expect(activityStore.loopLastSeen.has('loop-d')).toBe(true);
		expect(activityStore.loopLastSeen.has('loop-e')).toBe(true);

		tick({ type: 'loop_deleted', loop_id: 'loop-d' });
		expect(activityStore.loopLastSeen.has('loop-d')).toBe(false);
		expect(activityStore.loopLastSeen.has('loop-e')).toBe(true);
	});

	it('replaces the Map reference on every mutation (Svelte 5 reactivity contract)', () => {
		const ref0 = activityStore.loopLastSeen;
		tick({ type: 'loop_created', loop_id: 'loop-f' });
		const ref1 = activityStore.loopLastSeen;
		expect(ref1).not.toBe(ref0);

		tick({ type: 'loop_updated', loop_id: 'loop-f' });
		const ref2 = activityStore.loopLastSeen;
		expect(ref2).not.toBe(ref1);

		tick({ type: 'loop_deleted', loop_id: 'loop-f' });
		const ref3 = activityStore.loopLastSeen;
		expect(ref3).not.toBe(ref2);
	});

	it('does not create a new Map reference when deleting an absent loop', () => {
		// Eviction short-circuit: if the key isn't present, skip the replace.
		// Without this, every stray loop_deleted for an already-evicted loop
		// would invalidate LoopHeartbeat re-renders on all open plan cards.
		tick({ type: 'loop_created', loop_id: 'loop-g' });
		const ref0 = activityStore.loopLastSeen;

		tick({ type: 'loop_deleted', loop_id: 'loop-nonexistent' });
		expect(activityStore.loopLastSeen).toBe(ref0);
	});

	it('ignores events without loop_id', () => {
		const ref0 = activityStore.loopLastSeen;
		tick({ type: 'connection_established' });
		expect(activityStore.loopLastSeen).toBe(ref0);
	});

	describe('idleMsForLoop', () => {
		it('returns null for an unknown loop', () => {
			expect(activityStore.idleMsForLoop('nope')).toBeNull();
		});

		it('returns a positive delta for a known loop', async () => {
			tick({ type: 'loop_created', loop_id: 'loop-h' });
			await new Promise((r) => setTimeout(r, 10));
			const idle = activityStore.idleMsForLoop('loop-h');
			expect(idle).not.toBeNull();
			expect(idle!).toBeGreaterThanOrEqual(10);
		});

		it('returns null after the loop is evicted', () => {
			tick({ type: 'loop_created', loop_id: 'loop-i' });
			tick({ type: 'loop_deleted', loop_id: 'loop-i' });
			expect(activityStore.idleMsForLoop('loop-i')).toBeNull();
		});
	});
});
