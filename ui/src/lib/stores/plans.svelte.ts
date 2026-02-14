import { mockPlans, mockTasks } from '$lib/api/mock-plans';
import type { PlanWithStatus, PlanStage } from '$lib/types/plan';
import type { Task } from '$lib/types/task';

/**
 * Store for ADR-003 Plan + Tasks workflow.
 * Replaces the old changesStore.
 *
 * Currently uses mock data. Replace with real API calls when backend is ready:
 * - GET /api/workflow/plans
 * - GET /api/workflow/plans/{slug}
 * - GET /api/workflow/plans/{slug}/tasks
 * - POST /api/workflow/plans/{slug}/promote
 * - POST /api/workflow/plans/{slug}/tasks/generate
 * - POST /api/workflow/plans/{slug}/execute
 */
class PlansStore {
	all = $state<PlanWithStatus[]>([]);
	tasksByPlan = $state<Record<string, Task[]>>({});
	loading = $state(false);
	error = $state<string | null>(null);
	selectedSlug = $state<string | null>(null);

	/**
	 * Explorations (uncommitted plans)
	 */
	get explorations(): PlanWithStatus[] {
		return this.all.filter((p) => !p.committed);
	}

	/**
	 * Committed plans
	 */
	get committed(): PlanWithStatus[] {
		return this.all.filter((p) => p.committed);
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
			exploration: [],
			planning: [],
			tasks: [],
			executing: [],
			complete: [],
			failed: []
		};

		for (const plan of this.all) {
			grouped[plan.stage].push(plan);
		}

		return grouped;
	}

	/**
	 * Plans currently executing
	 */
	get executing(): PlanWithStatus[] {
		return this.all.filter((p) => p.stage === 'executing');
	}

	/**
	 * Plans with active loops
	 */
	get withActiveLoops(): PlanWithStatus[] {
		return this.all.filter((p) => p.active_loops.length > 0);
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
			// TODO: Replace with real API call
			// this.all = await api.plans.list();
			await new Promise((resolve) => setTimeout(resolve, 200));
			this.all = mockPlans;
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
			// TODO: Replace with real API call
			// const tasks = await api.plans.getTasks(slug);
			await new Promise((resolve) => setTimeout(resolve, 100));
			const tasks = mockTasks[slug] || [];
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
	 * Promote an exploration to a committed plan
	 */
	async promote(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan) return;

		try {
			// TODO: Replace with real API call
			// await api.plans.promote(slug);
			await new Promise((resolve) => setTimeout(resolve, 300));

			// Update local state
			plan.committed = true;
			plan.committed_at = new Date().toISOString();
			plan.stage = 'planning';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to promote plan';
		}
	}

	/**
	 * Generate tasks for a committed plan
	 */
	async generateTasks(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan || !plan.committed) return;

		try {
			// TODO: Replace with real API call
			// await api.plans.generateTasks(slug);
			await new Promise((resolve) => setTimeout(resolve, 500));

			// Update local state
			plan.stage = 'tasks';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to generate tasks';
		}
	}

	/**
	 * Start executing tasks for a plan
	 */
	async execute(slug: string): Promise<void> {
		const plan = this.getBySlug(slug);
		if (!plan || plan.stage !== 'tasks') return;

		try {
			// TODO: Replace with real API call
			// await api.plans.execute(slug);
			await new Promise((resolve) => setTimeout(resolve, 300));

			// Update local state
			plan.stage = 'executing';
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to start execution';
		}
	}
}

export const plansStore = new PlansStore();
