/**
 * Direct API helpers for test setup/teardown.
 * These bypass the UI to create known state before testing UI interactions.
 */

const API_BASE = 'http://localhost:3000';

export interface PlanResponse {
	slug: string;
	title: string;
	goal: string;
	stage: string;
	approved: boolean;
	created_at: string;
	[key: string]: unknown;
}

export async function createPlan(description: string): Promise<PlanResponse> {
	const res = await fetch(`${API_BASE}/plan-manager/plans`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ description })
	});
	if (!res.ok) {
		const err = await res.text();
		throw new Error(`Create plan failed (${res.status}): ${err}`);
	}
	return res.json();
}

export async function getPlan(slug: string): Promise<PlanResponse> {
	const res = await fetch(`${API_BASE}/plan-manager/plans/${slug}`);
	if (!res.ok) {
		throw new Error(`Get plan failed (${res.status})`);
	}
	return res.json();
}

export async function listPlans(): Promise<PlanResponse[]> {
	const res = await fetch(`${API_BASE}/plan-manager/plans`);
	if (!res.ok) {
		throw new Error(`List plans failed (${res.status})`);
	}
	return res.json();
}

export async function promotePlan(slug: string): Promise<PlanResponse> {
	const res = await fetch(`${API_BASE}/plan-manager/plans/${slug}/promote`, {
		method: 'POST'
	});
	if (!res.ok) {
		const err = await res.text();
		throw new Error(`Promote plan failed (${res.status}): ${err}`);
	}
	return res.json();
}

export async function executePlan(slug: string): Promise<PlanResponse> {
	const res = await fetch(`${API_BASE}/plan-manager/plans/${slug}/execute`, {
		method: 'POST'
	});
	if (!res.ok) {
		const err = await res.text();
		throw new Error(`Execute plan failed (${res.status}): ${err}`);
	}
	return res.json();
}

export async function deletePlan(slug: string): Promise<void> {
	await fetch(`${API_BASE}/plan-manager/plans/${slug}`, { method: 'DELETE' });
}

/** Wait for plan goal synthesis to complete (stage reaches ready_for_approval). */
export async function waitForGoal(slug: string, timeoutMs = 15000): Promise<PlanResponse> {
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		const plan = await getPlan(slug);
		if (plan.goal && plan.stage === 'ready_for_approval') return plan;
		await new Promise((r) => setTimeout(r, 500));
	}
	throw new Error(`Plan ${slug} goal not ready after ${timeoutMs}ms`);
}

export async function waitForBackendHealth(timeoutMs = 15000): Promise<void> {
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			const res = await fetch(`${API_BASE}/agentic-dispatch/health`);
			if (res.ok) return;
		} catch {
			// not ready yet
		}
		await new Promise((r) => setTimeout(r, 500));
	}
	throw new Error(`Backend not healthy after ${timeoutMs}ms`);
}
