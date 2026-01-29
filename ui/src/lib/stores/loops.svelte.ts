import { api } from '$lib/api/client';
import type { Loop } from '$lib/types';

class LoopsStore {
	all = $state<Loop[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);

	get active(): Loop[] {
		return this.all.filter((l) =>
			['executing', 'paused', 'awaiting_approval'].includes(l.state)
		);
	}

	get pendingReview(): Loop[] {
		return this.all.filter((l) => l.state === 'awaiting_approval');
	}

	get completedToday(): number {
		const today = new Date().toDateString();
		return this.all.filter(
			(l) => l.state === 'complete' && l.startedAt && new Date(l.startedAt).toDateString() === today
		).length;
	}

	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			const response = await api.router.getLoops();
			this.all = response.loops;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch loops';
		} finally {
			this.loading = false;
		}
	}

	async sendSignal(loopId: string, type: string, payload?: unknown): Promise<void> {
		await api.router.sendSignal(loopId, type, payload);
	}
}

export const loopsStore = new LoopsStore();
