import { api } from '$lib/api/client';
import type { Trajectory, TrajectoryListItem } from '$lib/types/trajectory';

/**
 * Store for trajectory data — agent loop execution history.
 * Caches trajectory data per loop_id to avoid redundant API calls.
 */
class TrajectoryStore {
	cache = $state<Record<string, Trajectory>>({});
	loading = $state<Record<string, boolean>>({});
	errors = $state<Record<string, string | null>>({});

	/** Lightweight summary cache keyed by loop_id — populated from list prefetch */
	summaries = $state<Record<string, TrajectoryListItem>>({});

	/** Recent trajectory list items for the trajectories list page */
	recentItems = $state<TrajectoryListItem[]>([]);
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
			const trajectory = await api.trajectory.getByLoop(loopId);
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
	 * List recent trajectory summaries using the trajectory list endpoint.
	 */
	async listRecent(limit = 50): Promise<void> {
		this.recentLoading = true;
		this.recentError = null;
		try {
			const result = await api.trajectory.list({ limit });
			this.recentItems = result.trajectories;
		} catch (err) {
			this.recentError = err instanceof Error ? err.message : 'Failed to fetch trajectories';
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
	 * Seed the summary cache from a trajectory list prefetch.
	 * Keyed by loop_id — used for quick display before full trajectory loads.
	 */
	seedFromList(items: TrajectoryListItem[]): void {
		const next: Record<string, TrajectoryListItem> = { ...this.summaries };
		for (const item of items) {
			next[item.loop_id] = item;
		}
		this.summaries = next;
	}

	/**
	 * Get a cached summary for a loop (returns undefined if not seeded).
	 */
	getSummary(loopId: string): TrajectoryListItem | undefined {
		return this.summaries[loopId];
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
