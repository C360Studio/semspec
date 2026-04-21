import type { PageLoad } from './$types';
import type { PlanWithStatus } from '$lib/types/plan';

/**
 * Client-side-only load for the /status route.
 *
 * SSR is disabled so this load always runs in the browser. This makes
 * Playwright's page.route() stubs effective: stubs intercept browser-side
 * fetches, not Node-side fetches. With ssr=false, there's no Node-side
 * fetch to bypass them.
 *
 * In production, the browser fetches plans directly from the backend
 * on first navigation and on any invalidate('app:plans') call.
 *
 * We only fetch plans — active loops and their turn counters come from the
 * plan's own `active_loops` field, not a separate /agentic-dispatch/loops
 * call. Adding a second fetch here would return the same loops on a wider
 * filter (all loops, not just active ones for implementing plans), which
 * wastes a request and adds a merge step.
 */
export const ssr = false;

export const load: PageLoad = async ({ fetch, depends }) => {
	depends('app:plans');

	const plans = await fetch('/plan-manager/plans')
		.then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus[]>) : []))
		.catch(() => [] as PlanWithStatus[]);

	return { statusPlans: plans };
};
