import { api } from '$lib/api/client';
import type { Loop } from '$lib/types';

class LoopsStore {
	all = $state<Loop[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);

	get active(): Loop[] {
		return this.all.filter((l) => ['pending', 'executing', 'paused'].includes(l.state));
	}

	get paused(): Loop[] {
		return this.all.filter((l) => l.state === 'paused');
	}

	get completedToday(): number {
		const today = new Date().toDateString();
		return this.all.filter(
			(l) =>
				l.state === 'complete' &&
				l.created_at &&
				new Date(l.created_at).toDateString() === today
		).length;
	}

	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			// Backend returns Loop[] directly (not wrapped)
			this.all = await api.router.getLoops();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch loops';
		} finally {
			this.loading = false;
		}
	}

	async sendSignal(loopId: string, type: 'pause' | 'resume' | 'cancel', reason?: string): Promise<void> {
		await api.router.sendSignal(loopId, type, reason);
	}
}

export const loopsStore = new LoopsStore();
