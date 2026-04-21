import type { AttentionItem } from '$lib/api/mock-plans';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';
import type { Loop } from '$lib/types';

/**
 * Attention item types
 */
export type AttentionType =
	| 'approval_needed'
	| 'task_failed'
	| 'task_blocked'
	| 'rejection';

/**
 * Compute attention items from plans, loops, and tasks.
 * Pure function — no store dependency.
 *
 * `loops` is kept in the signature for backward compatibility but is only
 * used for the `rejection` path via tasksByPlan. Loop-level `state=failed`
 * is NOT surfaced as an alarm because a failed dev loop mid-TDD is actively
 * being retried — not user-actionable. Only a requirement whose retry budget
 * is exhausted (plan.execution_summary.failed > 0, while stage is still
 * implementing) deserves the banner.
 */
export function computeAttentionItems(
	plans: PlanWithStatus[],
	_loops: Loop[],
	tasksByPlan?: Record<string, Task[]>
): AttentionItem[] {
	const result: AttentionItem[] = [];

	// Plans ready to execute (tasks approved)
	for (const plan of plans.filter((p) => p.stage === 'tasks_approved')) {
		result.push({
			type: 'approval_needed',
			plan_slug: plan.slug,
			title: `Ready to execute "${plan.slug}"`,
			description: 'Tasks have been generated. Approve to begin execution.',
			action_url: `/plans/${plan.slug}`,
			created_at: plan.approved_at || plan.created_at
		});
	}

	// Plans with active rejections
	if (tasksByPlan) {
		for (const plan of plans) {
			const tasks = tasksByPlan[plan.slug] ?? [];
			const rejectedTask = tasks.find((t) => t.rejection && t.status === 'in_progress');
			if (rejectedTask && rejectedTask.rejection) {
				result.push({
					type: 'rejection',
					plan_slug: plan.slug,
					title: `Task rejected in "${plan.slug}"`,
					description: rejectedTask.rejection.reason,
					action_url: `/plans/${plan.slug}`,
					created_at: rejectedTask.rejection.rejected_at
				});
			}
		}
	}

	// Requirement-level terminal failures — only while the plan is still
	// in-flight (implementing). Once the plan rolls up to complete/failed/
	// rejected, its own card communicates the state; duplicating it in the
	// banner adds noise without adding action.
	for (const p of plans) {
		if (p.stage !== 'implementing') continue;
		const failed = p.execution_summary?.failed ?? 0;
		if (failed <= 0) continue;

		const total = p.execution_summary?.total ?? 0;
		result.push({
			type: 'task_failed',
			plan_slug: p.slug,
			title: `${failed} of ${total} requirements failed in "${p.slug}"`,
			description: `Retry budget exhausted on ${failed} requirement${failed === 1 ? '' : 's'}. Intervene or let the plan roll up.`,
			action_url: `/plans/${p.slug}`,
			created_at: p.approved_at || p.created_at
		});
	}

	return result.sort(
		(a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
	);
}
