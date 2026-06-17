import type { PageLoad } from './$types';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Requirement } from '$lib/types/requirement';
import type { Scenario } from '$lib/types/scenario';
import type { TrajectoryListItem, TrajectoryListResponse } from '$lib/types/trajectory';
import type { SynthesisResult } from '$lib/types/review';
import type { ExecutionTask, Lesson } from '$lib/components/plan/observabilityModels';

export const load: PageLoad = async ({ params, fetch, depends }) => {
	depends('app:plans');
	const slug = params.slug;

	// Fetch plan, requirements, trajectory summaries, reviews, and execution state in parallel
	// Backend may return JSON `null` for empty collections, so coalesce to []
	const [plan, requirements, trajectoryItems, reviews, executionTasks, lessons] = await Promise.all([
		fetch(`/plan-manager/plans/${slug}`)
			.then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus>) : null))
			.catch(() => null),
		fetch(`/plan-manager/plans/${slug}/requirements`)
			.then((r) => (r.ok ? r.json().then((d: Requirement[] | null) => d ?? []) : []))
			.catch(() => [] as Requirement[]),
		fetch(`/agentic-loop/trajectories?metadata_key=plan_slug&metadata_value=${encodeURIComponent(slug)}&limit=50`)
			.then((r) =>
				r.ok
					? r.json().then((d: TrajectoryListResponse | null) => d?.trajectories ?? [])
					: []
			)
			.catch(() => [] as TrajectoryListItem[]),
		fetch(`/plan-manager/plans/${slug}/reviews`)
			.then((r) => (r.ok ? r.json().then((d: SynthesisResult | null) => d) : null))
			.catch(() => null),
		fetch(`/execution-manager/plans/${encodeURIComponent(slug)}/tasks`)
			.then((r) => (r.ok ? r.json().then((d: ExecutionTask[] | null) => d ?? []) : []))
			.catch(() => [] as ExecutionTask[]),
		fetch('/execution-manager/lessons?role=developer')
			.then((r) => (r.ok ? r.json().then((d: Lesson[] | null) => d ?? []) : []))
			.catch(() => [] as Lesson[])
	]);

	// Fetch scenarios for each requirement in parallel
	const scenarioEntries = await Promise.all(
		(requirements ?? []).map(async (req) => {
			const scenarios = await fetch(
				`/plan-manager/plans/${slug}/scenarios?requirement_id=${encodeURIComponent(req.id)}`
			)
				.then((r) => (r.ok ? r.json().then((d: Scenario[] | null) => d ?? []) : []))
				.catch(() => [] as Scenario[]);
			return [req.id, scenarios] as const;
		})
	);

	const scenariosByReq: Record<string, Scenario[]> = {};
	for (const [reqId, scenarios] of scenarioEntries) {
		scenariosByReq[reqId] = scenarios;
	}

	return {
		plan,
		requirements,
		scenariosByReq,
		trajectoryItems: trajectoryItems ?? [],
		reviews: reviews ?? null,
		executionTasks: executionTasks ?? [],
		lessons: lessons ?? []
	};
};
