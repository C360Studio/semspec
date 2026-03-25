import type { PageLoad } from './$types';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';
import type { Requirement } from '$lib/types/requirement';
import type { Scenario } from '$lib/types/scenario';

export const load: PageLoad = async ({ params, fetch, depends }) => {
	depends('app:plans');
	const slug = params.slug;

	// Fetch plan, tasks, and requirements in parallel
	const [plan, tasks, requirements] = await Promise.all([
		fetch(`/plan-manager/plans/${slug}`)
			.then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus>) : null))
			.catch(() => null),
		fetch(`/plan-manager/plans/${slug}/tasks`)
			.then((r) => (r.ok ? (r.json() as Promise<Task[]>) : []))
			.catch(() => [] as Task[]),
		fetch(`/plan-manager/plans/${slug}/requirements`)
			.then((r) => (r.ok ? (r.json() as Promise<Requirement[]>) : []))
			.catch(() => [] as Requirement[])
	]);

	// Fetch scenarios for each requirement in parallel
	const scenarioEntries = await Promise.all(
		requirements.map(async (req) => {
			const scenarios = await fetch(
				`/plan-manager/plans/${slug}/scenarios?requirement_id=${encodeURIComponent(req.id)}`
			)
				.then((r) => (r.ok ? (r.json() as Promise<Scenario[]>) : []))
				.catch(() => [] as Scenario[]);
			return [req.id, scenarios] as const;
		})
	);

	const scenariosByReq: Record<string, Scenario[]> = {};
	for (const [reqId, scenarios] of scenarioEntries) {
		scenariosByReq[reqId] = scenarios;
	}

	return { plan, tasks, requirements, scenariosByReq };
};
