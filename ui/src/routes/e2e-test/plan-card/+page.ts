import type { PageLoad } from './$types';
import type { PlanWithStatus } from '$lib/types/plan';

/**
 * SSR disabled so Playwright stubs intercept every fetch. The /board view
 * has a +layout.server.ts that fetches plans server-side — impossible to
 * stub from Playwright — so the plan-card-status truth-tests used to race
 * real backend state. This harness renders PlanCard directly off the
 * stubbed /plan-manager/plans response, mirroring the /status pattern.
 */
export const ssr = false;

export const load: PageLoad = async ({ fetch }) => {
	const plans = await fetch('/plan-manager/plans')
		.then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus[]>) : []))
		.catch(() => [] as PlanWithStatus[]);
	return { plans };
};
