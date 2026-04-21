import type { PageLoad } from './$types';
import type { TrajectoryListItem } from '$lib/types/trajectory';

export const load: PageLoad = async ({ fetch, depends }) => {
	depends('app:trajectories');

	try {
		const res = await fetch('/agentic-loop/trajectories?limit=50');
		if (!res.ok) throw new Error(`HTTP ${res.status}`);
		const result = (await res.json()) as { trajectories: TrajectoryListItem[] };
		return { trajectories: result.trajectories ?? [] };
	} catch {
		return { trajectories: [] as TrajectoryListItem[] };
	}
};
