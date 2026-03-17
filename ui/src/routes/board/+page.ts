import type { PageLoad } from './$types';
import type { Task } from '$lib/types/task';

export const load: PageLoad = async ({ parent, fetch, depends }) => {
	depends('app:board');

	const { plans } = await parent();
	const activePlans = (plans ?? []).filter(
		(p) => !['complete', 'failed', 'archived'].includes(p.stage)
	);

	// Fetch tasks for each active plan in parallel
	const taskEntries = await Promise.all(
		activePlans.map(async (p) => {
			const tasks = await fetch(`/workflow-api/plans/${p.slug}/tasks`)
				.then((r) => (r.ok ? (r.json() as Promise<Task[]>) : []))
				.catch(() => [] as Task[]);
			return [p.slug, tasks] as const;
		})
	);

	const tasksByPlan: Record<string, Task[]> = {};
	for (const [slug, tasks] of taskEntries) {
		tasksByPlan[slug] = tasks;
	}

	return { tasksByPlan };
};
