/**
 * Plan mutation actions.
 *
 * Each function calls the API then invalidates the relevant SvelteKit
 * data dependencies so the server load re-runs and fresh data flows
 * through the component tree automatically.
 */
import { invalidate } from '$app/navigation';
import { api } from '$lib/api/client';

export async function promotePlan(slug: string): Promise<void> {
	await api.plans.promote(slug);
	await invalidate('app:plans');
}

export async function executePlan(slug: string): Promise<void> {
	await api.plans.execute(slug);
	await invalidate('app:plans');
}

export async function generateTasks(slug: string): Promise<void> {
	await api.plans.generateTasks(slug);
	await invalidate('app:plans');
}

export async function approveTasks(slug: string): Promise<void> {
	await api.plans.approveTasks(slug);
	await invalidate('app:plans');
}

export async function retryFailed(slug: string): Promise<void> {
	await api.plans.retry(slug, 'failed');
	await invalidate('app:plans');
}

/**
 * Retry a specific subset of requirements. Preserves already-completed and
 * still-running requirements untouched — lets the user cherry-pick which
 * failures to retry instead of the blanket "retry all failed" action.
 */
export async function retrySelected(slug: string, requirementIds: string[]): Promise<void> {
	if (requirementIds.length === 0) {
		throw new Error('No requirements selected to retry');
	}
	await api.plans.retry(slug, 'requirements', requirementIds);
	await invalidate('app:plans');
}
