import type { PageLoad } from './$types';
import type { Trajectory } from '$lib/types/trajectory';

export const load: PageLoad = async ({ params, fetch, parent }) => {
	const loopId = params.id;

	// Trajectory is page-specific; loops come from the layout cascade
	const [trajectory, { loops }] = await Promise.all([
		fetch(`/agentic-loop/trajectories/${loopId}?format=json`)
			.then((r) => (r.ok ? (r.json() as Promise<Trajectory>) : null))
			.catch(() => null),
		parent()
	]);

	const loop = (loops ?? []).find((l) => l.loop_id === loopId) ?? null;

	return { loopId, trajectory, loop };
};
