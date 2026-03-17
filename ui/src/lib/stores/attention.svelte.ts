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
 */
export function computeAttentionItems(
	plans: PlanWithStatus[],
	loops: Loop[],
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

	// Failed loops
	for (const loop of loops.filter((l) => l.state === 'failed')) {
		const plan = plans.find((p) =>
			p.active_loops?.some((al) => al.loop_id === loop.loop_id)
		);

		result.push({
			type: 'task_failed',
			loop_id: loop.loop_id,
			plan_slug: plan?.slug,
			title: `Task failed in loop ${loop.loop_id.slice(-6)}`,
			description: `Loop failed after ${loop.iterations} iterations`,
			action_url: plan ? `/plans/${plan.slug}` : '/activity',
			created_at: loop.created_at || new Date().toISOString()
		});
	}

	return result.sort(
		(a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
	);
}
