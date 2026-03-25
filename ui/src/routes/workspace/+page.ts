import type { PageLoad } from './$types';
import type { WorkspaceTask } from '$lib/api/workspace';

export const load: PageLoad = async ({ url, fetch }) => {
	const taskIdParam = url.searchParams.get('task_id');

	const tasks = await fetch('/plan-manager/workspace/tasks')
		.then((r) => (r.ok ? (r.json() as Promise<WorkspaceTask[]>) : []))
		.catch(() => [] as WorkspaceTask[]);

	return { tasks, taskIdParam };
};
