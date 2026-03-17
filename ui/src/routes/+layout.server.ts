import type { LayoutServerLoad } from './$types';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Loop, SystemHealth } from '$lib/types';

export const load: LayoutServerLoad = async ({ fetch, depends }) => {
	depends('app:plans');
	depends('app:loops');
	depends('app:system');

	const [plans, loops, system] = await Promise.all([
		fetch('/workflow-api/plans')
			.then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus[]>) : []))
			.catch(() => [] as PlanWithStatus[]),
		fetch('/agentic-dispatch/loops')
			.then((r) => (r.ok ? (r.json() as Promise<Loop[]>) : []))
			.catch(() => [] as Loop[]),
		fetch('/agentic-dispatch/health')
			.then((r) => (r.ok ? (r.json() as Promise<SystemHealth>) : { healthy: false, components: [] }))
			.catch(() => ({ healthy: false, components: [] }) as SystemHealth)
	]);

	return { plans, loops, system };
};
