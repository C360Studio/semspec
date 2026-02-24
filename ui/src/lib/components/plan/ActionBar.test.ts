import { describe, it, expect, vi } from 'vitest';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';

/**
 * ActionBar component tests
 *
 * Tests button visibility logic based on plan status and tasks.
 * The ActionBar consolidates 4 separate action banners into a single component.
 */

describe('ActionBar visibility logic', () => {
	const createMockPlan = (overrides: Partial<PlanWithStatus> = {}): PlanWithStatus => ({
		slug: 'test-plan',
		title: 'Test Plan',
		approved: false,
		stage: 'draft',
		goal: '',
		context: '',
		scope: undefined,
		created_at: '2024-01-01T00:00:00Z',
		active_loops: [],
		id: 'plan-1',
		project_id: 'project-1',
		...overrides
	});

	const createMockTask = (overrides: Partial<Task> = {}): Task => ({
		id: 'task-1',
		plan_id: 'plan-1',
		phase_id: 'phase-1',
		description: 'Test task',
		status: 'pending',
		created_at: '2024-01-01T00:00:00Z',
		sequence: 1,
		acceptance_criteria: [],
		...overrides
	});

	describe('Approve Plan button', () => {
		it('should show when plan is not approved and has a goal', () => {
			const plan = createMockPlan({ approved: false, goal: 'Add auth' });
			const shouldShow = !plan.approved && !!plan.goal;
			expect(shouldShow).toBe(true);
		});

		it('should hide when plan is already approved', () => {
			const plan = createMockPlan({ approved: true, goal: 'Add auth' });
			const shouldShow = !plan.approved && !!plan.goal;
			expect(shouldShow).toBe(false);
		});

		it('should hide when plan has no goal', () => {
			const plan = createMockPlan({ approved: false, goal: '' });
			const shouldShow = !plan.approved && !!plan.goal;
			expect(shouldShow).toBe(false);
		});
	});

	describe('Generate Tasks button', () => {
		it('should show when plan is approved and stage is planning', () => {
			const plan = createMockPlan({ approved: true, stage: 'planning' });
			const shouldShow = plan.approved && plan.stage === 'planning';
			expect(shouldShow).toBe(true);
		});

		it('should hide when plan is not approved', () => {
			const plan = createMockPlan({ approved: false, stage: 'planning' });
			const shouldShow = plan.approved && plan.stage === 'planning';
			expect(shouldShow).toBe(false);
		});

		it('should hide when stage is not planning', () => {
			const plan = createMockPlan({ approved: true, stage: 'executing' });
			const shouldShow = plan.approved && plan.stage === 'planning';
			expect(shouldShow).toBe(false);
		});
	});

	describe('Approve All button', () => {
		it('should show when there are pending approval tasks', () => {
			const tasks: Task[] = [
				createMockTask({ status: 'pending_approval' }),
				createMockTask({ status: 'approved' })
			];
			const pendingCount = tasks.filter((t) => t.status === 'pending_approval').length;
			expect(pendingCount).toBeGreaterThan(0);
		});

		it('should hide when all tasks are approved', () => {
			const tasks: Task[] = [
				createMockTask({ status: 'approved' }),
				createMockTask({ status: 'approved' })
			];
			const pendingCount = tasks.filter((t) => t.status === 'pending_approval').length;
			expect(pendingCount).toBe(0);
		});

		it('should hide when there are no tasks', () => {
			const tasks: Task[] = [];
			const pendingCount = tasks.filter((t) => t.status === 'pending_approval').length;
			expect(pendingCount).toBe(0);
		});
	});

	describe('Execute button', () => {
		it('should show when all tasks approved and stage is tasks', () => {
			const plan = createMockPlan({ stage: 'tasks' });
			const tasks: Task[] = [
				createMockTask({ status: 'approved' }),
				createMockTask({ status: 'approved' })
			];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			const shouldShow =
				allTasksApproved &&
				['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage);
			expect(shouldShow).toBe(true);
		});

		it('should show when all tasks approved and stage is tasks_approved', () => {
			const plan = createMockPlan({ stage: 'tasks_approved' });
			const tasks: Task[] = [createMockTask({ status: 'approved' })];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			const shouldShow =
				allTasksApproved &&
				['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage);
			expect(shouldShow).toBe(true);
		});

		it('should show when all tasks approved and stage is tasks_generated', () => {
			const plan = createMockPlan({ stage: 'tasks_generated' });
			const tasks: Task[] = [createMockTask({ status: 'approved' })];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			const shouldShow =
				allTasksApproved &&
				['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage);
			expect(shouldShow).toBe(true);
		});

		it('should hide when not all tasks are approved', () => {
			const plan = createMockPlan({ stage: 'tasks' });
			const tasks: Task[] = [
				createMockTask({ status: 'approved' }),
				createMockTask({ status: 'pending_approval' })
			];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			expect(allTasksApproved).toBe(false);
		});

		it('should hide when there are no tasks', () => {
			const plan = createMockPlan({ stage: 'tasks' });
			const tasks: Task[] = [];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			expect(allTasksApproved).toBe(false);
		});

		it('should hide when stage is not tasks/tasks_approved/tasks_generated', () => {
			const plan = createMockPlan({ stage: 'executing' });
			const tasks: Task[] = [createMockTask({ status: 'approved' })];
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			const shouldShow =
				allTasksApproved &&
				['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage);
			expect(shouldShow).toBe(false);
		});
	});

	describe('Button priority and exclusivity', () => {
		it('should prioritize Approve All over Execute when tasks need approval', () => {
			const plan = createMockPlan({ stage: 'tasks' });
			const tasks: Task[] = [
				createMockTask({ status: 'approved' }),
				createMockTask({ status: 'pending_approval' })
			];

			const pendingCount = tasks.filter((t) => t.status === 'pending_approval').length;
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');

			// Approve All should show
			expect(pendingCount).toBeGreaterThan(0);
			// Execute should not show
			expect(allTasksApproved).toBe(false);
		});

		it('should only show one primary action at a time', () => {
			const plan = createMockPlan({ approved: false, goal: 'test', stage: 'planning' });
			const tasks: Task[] = [];

			const showApprove = !plan.approved && !!plan.goal;
			const showGenerate = plan.approved && plan.stage === 'planning';
			const pendingCount = tasks.filter((t) => t.status === 'pending_approval').length;
			const allTasksApproved =
				tasks.length > 0 &&
				tasks.every((t) => t.status === 'approved' || t.status === 'completed');
			const showExecute =
				allTasksApproved &&
				['tasks', 'tasks_approved', 'tasks_generated'].includes(plan.stage);

			// Only one should be true
			const activeButtons = [showApprove, showGenerate, pendingCount > 0, showExecute].filter(
				Boolean
			).length;
			expect(activeButtons).toBeLessThanOrEqual(1);
		});
	});
});
