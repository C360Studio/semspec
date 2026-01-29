import { api } from '$lib/api/client';
import type { SystemHealth, ComponentHealth } from '$lib/types';

class SystemStore {
	healthy = $state(true);
	components = $state<ComponentHealth[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);

	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			const response = (await api.system.getHealth()) as SystemHealth;
			this.healthy = response.healthy;
			this.components = response.components;
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch system health';
			this.healthy = false;
		} finally {
			this.loading = false;
		}
	}
}

export const systemStore = new SystemStore();
