import { api } from '$lib/api/client';
import type { PlanWithStatus, PlanStage } from '$lib/types/plan';
import type { Task } from '$lib/types/task';

/**
 * Store for ADR-003 Plan + Tasks workflow.
 * Replaces the old changesStore.
 *
 * API endpoints:
 * - GET /workflow-api/plans
 * - GET /workflow-api/plans/{slug}
 * - GET /workflow-api/plans/{slug}/tasks
 * - POST /workflow-api/plans/{slug}/promote
 * - POST /workflow-api/plans/{slug}/tasks/generate
 * - POST /workflow-api/plans/{slug}/execute
 */
class PlansStore {
	all = $state<PlanWithStatus[]>([]);
	tasksByPlan = $state<Record<string, Task[]>>({});
	loading = $state(false);
	error = $state<string | null>(null);
	selectedSlug = $state<string | null>(null);

	/**
	 * Draft plans (not yet approved)
	 */
	get drafts(): PlanWithStatus[] {
		return this.all.filter((p) => !p.approved);
	}

	/**
	 * Approved plans
	 */
	get approved(): PlanWithStatus[] {
		return this.all.filter((p) => p.approved);
	}

	/**
	 * Active plans (not complete or failed)
	 */
	get active(): PlanWithStatus[] {
		return this.all.filter((p) => !['complete', 'failed'].includes(p.stage));
	}

	/**
	 * Plans grouped by stage
	 */
	get byStage(): Record<PlanStage, PlanWithStatus[]> {
		const grouped: Record<PlanStage, PlanWithStatus[]> = {
			draft: [],
			drafting: [],
			ready_for_approval: [],
			reviewed: [],
			needs_changes: [],
			planning: [],
			approved: [],
			rejected: [],
			phases_generated: [],
			phases_approved: [],
			tasks_generated: [],
			tasks_approved: [],
			tasks: [],
			implementing: [],
			executing: [],
			complete: [],
			archived: [],
			failed: []
		};

		for (const plan of this.all) {
			if (plan.stage in grouped) {
				grouped[plan.stage].push(plan);
			}
		}

		return grouped;
	}

	/**
	 * Plans currently executing
	 */
	get executing(): PlanWithStatus[] {
		return this.all.filter((p) => p.stage === 'implementing' || p.stage === 'executing');
	}

	/**
	 * Plans with active loops
	 */
	get withActiveLoops(): PlanWithStatus[] {
		return this.all.filter((p) => (p.active_loops?.length ?? 0) > 0);
	}

	/**
	 * Get a single plan by slug
	 */
	getBySlug(slug: string): PlanWithStatus | undefined {
		return this.all.find((p) => p.slug === slug);
	}

	/**
	 * Fetch all plans
	 */
	async fetch(): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.all = await api.plans.list();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch plans';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Fetch tasks for a specific plan
	 */
	async fetchTasks(slug: string): Promise<Task[]> {
		try {
			const tasks = await api.plans.getTasks(slug);
			this.tasksByPlan[slug] = tasks;
			return tasks;
		} catch (err) {
			console.error('Failed to fetch tasks:', err);
			return [];
		}
	}

	/**
	 * Get cached tasks for a plan
	 */
	getTasks(slug: string): Task[] {
		return this.tasksByPlan[slug] || [];
	}

	/**
	 * Approve a draft plan
	 */
	async promote(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan) return;

		try {
			const updated = await api.plans.promote(slug);
			// Update local state with response
			Object.assign(plan, updated);
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to approve plan';
		}
	}

	/**
	 * Generate tasks for an approved plan
	 */
	async generateTasks(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan || !plan.approved) return;

		try {
			const tasks = await api.plans.generateTasks(slug);
			this.tasksByPlan[slug] = tasks;
			// Update local state
			plan.stage = 'tasks_generated';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to generate tasks';
		}
	}

	/**
	 * Start executing tasks for a plan
	 */
	async execute(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan || plan.stage !== 'tasks_approved') return;

		try {
			const updated = await api.plans.execute(slug);
			// Update local state with response
			Object.assign(plan, updated);
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to start execution';
		}
	}
}

export const plansStore = new PlansStore();
