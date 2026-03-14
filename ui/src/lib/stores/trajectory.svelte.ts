import { api } from '$lib/api/client';
import type { Loop } from '$lib/types';
import type { Trajectory } from '$lib/types/trajectory';

/** A trajectory list entry combining loop state with cached trajectory data. */
export interface TrajectoryListEntry {
	loopId: string;
	loop: Loop;
	trajectory?: Trajectory;
}

/**
 * Store for trajectory data — agent loop execution history.
 * Caches trajectory data per loop_id to avoid redundant API calls.
 */
class TrajectoryStore {
	cache = $state<Record<string, Trajectory>>({});
	loading = $state<Record<string, boolean>>({});
	errors = $state<Record<string, string | null>>({});

	/** Recent loops list for the trajectories list page */
	recentLoops = $state<Loop[]>([]);
	recentLoading = $state(false);
	recentError = $state<string | null>(null);

	/**
	 * Fetch trajectory for a loop, caching the result.
	 */
	async fetch(loopId: string): Promise<Trajectory | null> {
		if (this.cache[loopId]) return this.cache[loopId];

		this.loading[loopId] = true;
		this.errors[loopId] = null;

		try {
			const trajectory = await api.trajectory.getByLoop(loopId, 'json');
			this.cache[loopId] = trajectory;
			return trajectory;
		} catch (err) {
			this.errors[loopId] = err instanceof Error ? err.message : 'Failed to fetch trajectory';
			return null;
		} finally {
			this.loading[loopId] = false;
		}
	}

	/**
	 * List recent loops that have trajectory data.
	 * Uses the agentic-dispatch loops endpoint (same data source as activity page).
	 */
	async listRecent(limit = 50): Promise<void> {
		this.recentLoading = true;
		this.recentError = null;
		try {
			const loops = await api.router.getLoops();
			// Sort by created_at descending, take limit
			const sorted = [...loops].sort((a, b) => {
				const ta = a.created_at ? new Date(a.created_at).getTime() : 0;
				const tb = b.created_at ? new Date(b.created_at).getTime() : 0;
				return tb - ta;
			});
			this.recentLoops = sorted.slice(0, limit);
		} catch (err) {
			this.recentError = err instanceof Error ? err.message : 'Failed to fetch loops';
		} finally {
			this.recentLoading = false;
		}
	}

	/**
	 * Get cached trajectory for a loop (returns undefined if not fetched).
	 */
	get(loopId: string): Trajectory | undefined {
		return this.cache[loopId];
	}

	/**
	 * Check if a trajectory is currently loading.
	 */
	isLoading(loopId: string): boolean {
		return this.loading[loopId] ?? false;
	}

	/**
	 * Get error for a trajectory fetch.
	 */
	getError(loopId: string): string | null {
		return this.errors[loopId] ?? null;
	}

	/**
	 * Clear cached trajectory for a loop (forces re-fetch on next access).
	 */
	invalidate(loopId: string): void {
		delete this.cache[loopId];
		delete this.loading[loopId];
		delete this.errors[loopId];
	}

	/**
	 * Clear all cached trajectories.
	 */
	clear(): void {
		this.cache = {};
		this.loading = {};
		this.errors = {};
	}
}

export const trajectoryStore = new TrajectoryStore();
