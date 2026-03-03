import { test, expect, mockPlan, mockPhase, mockTask } from './helpers/setup';

/**
 * Tests for Plan detail page UX:
 * - Resizable panels (PlanNavTree + DetailPanel)
 * - Tree navigation (phases and tasks)
 * - Task approval workflow (via TaskDetail)
 * - Plan inline editing
 *
 * The UI uses a tree navigation layout:
 *   Left panel: PlanNavTree (plan > phases > tasks)
 *   Right-top panel: PlanDetailPanel (PlanDetail | PhaseDetail | TaskDetail)
 *   Right-bottom panel: ChatPanel
 */

// Shared mock data builders
function buildPlanRoutes(
	page: import('@playwright/test').Page,
	plan: ReturnType<typeof mockPlan>,
	phases: ReturnType<typeof mockPhase>[],
	tasks: ReturnType<typeof mockTask>[]
) {
	return Promise.all([
		page.route('**/workflow-api/plans', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		}),
		page.route(`**/workflow-api/plans/${plan.slug}`, (route) => {
			if (route.request().method() === 'GET') {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(plan)
				});
			} else {
				route.continue();
			}
		}),
		page.route(`**/workflow-api/plans/${plan.slug}/phases`, (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(phases)
			});
		}),
		page.route(`**/workflow-api/plans/${plan.slug}/tasks`, (route) => {
			if (route.request().method() === 'GET') {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(tasks)
				});
			} else {
				route.continue();
			}
		})
	]);
}

test.describe('Plan Detail UX', () => {
	test.describe('Resizable Panels', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: 'test-panels-plan',
				title: 'Test Panels Plan',
				goal: 'Test resizable panels',
				approved: true,
				stage: 'phases_approved'
			});

			const phase = mockPhase({
				id: 'phase-1',
				name: 'Phase 1',
				sequence: 1,
				status: 'ready',
				approved: true
			});

			await buildPlanRoutes(page, plan, [phase], []);
			await planDetailPage.goto('test-panels-plan');
		});

		test('shows resizable split layout with nav and detail panels', async ({ planDetailPage }) => {
			await planDetailPage.expectResizableSplitVisible();
			await planDetailPage.expectPlanPanelVisible();
			await planDetailPage.expectTasksPanelVisible();
		});

		test('shows resize divider between panels', async ({ planDetailPage }) => {
			await planDetailPage.expectResizableSplitVisible();
			await planDetailPage.expectResizeDividerVisible();
		});
	});

	test.describe('Tree Navigation', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: 'test-tree-plan',
				title: 'Test Tree Plan',
				approved: true,
				stage: 'phases_approved'
			});

			const phases = [
				mockPhase({ id: 'phase-1', name: 'Setup Phase', sequence: 1, status: 'ready', approved: true }),
				mockPhase({ id: 'phase-2', name: 'Implementation Phase', sequence: 2, status: 'pending', approved: true })
			];

			const tasks = [
				mockTask({ id: 'task-1', description: 'Configure environment', sequence: 1, status: 'pending', phase_id: 'phase-1' }),
				mockTask({ id: 'task-2', description: 'Write tests', sequence: 2, status: 'pending', phase_id: 'phase-1' }),
				mockTask({ id: 'task-3', description: 'Implement feature', sequence: 1, status: 'pending', phase_id: 'phase-2' })
			];

			await buildPlanRoutes(page, plan, phases, tasks);
			await planDetailPage.goto('test-tree-plan');
		});

		test('shows navigation tree with phases', async ({ planDetailPage }) => {
			await planDetailPage.expectNavTreeVisible();
			await planDetailPage.expectPhaseInTree('Setup Phase');
			await planDetailPage.expectPhaseInTree('Implementation Phase');
		});

		test('selecting a phase shows PhaseDetail', async ({ planDetailPage, page }) => {
			await planDetailPage.selectPhaseInTree('Setup Phase');
			await expect(page.locator('.phase-detail .detail-title')).toHaveText('Setup Phase');
		});

		test('expanding a phase shows its tasks', async ({ planDetailPage }) => {
			await planDetailPage.expandPhaseInTree('Setup Phase');
			await planDetailPage.expectTaskInTree('Configure environment');
			await planDetailPage.expectTaskInTree('Write tests');
		});

		test('selecting a task shows TaskDetail', async ({ planDetailPage }) => {
			await planDetailPage.expandPhaseInTree('Setup Phase');
			await planDetailPage.selectTaskInTree('Configure environment');
			await planDetailPage.expectTaskDetailVisible();
			await planDetailPage.expectTaskDetailTitle('Configure environment');
		});
	});

	test.describe('Task Approval', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: 'test-approval-plan',
				title: 'Test Approval Plan',
				approved: true,
				stage: 'tasks_generated'
			});

			const phases = [
				mockPhase({ id: 'phase-1', name: 'Phase 1', sequence: 1, status: 'active', approved: true })
			];

			const tasks = [
				mockTask({
					id: 'task-1',
					description: 'Implement authentication',
					sequence: 1,
					status: 'pending_approval',
					type: 'implement',
					phase_id: 'phase-1',
					acceptance_criteria: [
						{ given: 'a user with valid credentials', when: 'they log in', then: 'they should see the dashboard' }
					]
				}),
				mockTask({
					id: 'task-2',
					description: 'Write unit tests',
					sequence: 2,
					status: 'pending_approval',
					type: 'test',
					phase_id: 'phase-1'
				})
			];

			await buildPlanRoutes(page, plan, phases, tasks);
			await planDetailPage.goto('test-approval-plan');
		});

		test('shows Approve All button when tasks pending approval', async ({ planDetailPage }) => {
			await planDetailPage.expectActionBarVisible();
			await planDetailPage.expectApproveAllBtnVisible();
		});

		test('selecting a pending_approval task shows approve/reject buttons', async ({ planDetailPage }) => {
			await planDetailPage.expandPhaseInTree('Phase 1');
			await planDetailPage.selectTaskInTree('Implement authentication');
			await planDetailPage.expectTaskApproveVisible();
		});

		test('can approve a task via TaskDetail', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans/test-approval-plan/tasks/task-1/approve', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(
						mockTask({ id: 'task-1', description: 'Implement authentication', status: 'approved', phase_id: 'phase-1' })
					)
				});
			});

			await planDetailPage.expandPhaseInTree('Phase 1');
			await planDetailPage.selectTaskInTree('Implement authentication');
			await planDetailPage.clickTaskApprove();
		});

		test('can reject a task with reason via TaskDetail', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans/test-approval-plan/tasks/task-1/reject', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(
						mockTask({
							id: 'task-1',
							description: 'Implement authentication',
							status: 'rejected',
							phase_id: 'phase-1'
						})
					)
				});
			});

			await planDetailPage.expandPhaseInTree('Phase 1');
			await planDetailPage.selectTaskInTree('Implement authentication');
			await planDetailPage.clickTaskReject();
			await planDetailPage.fillTaskRejectReason('Needs more detail');
			await planDetailPage.confirmTaskReject();
		});

		test('task detail shows acceptance criteria', async ({ planDetailPage }) => {
			await planDetailPage.expandPhaseInTree('Phase 1');
			await planDetailPage.selectTaskInTree('Implement authentication');
			await planDetailPage.expectAcceptanceCriteria();
		});
	});

	test.describe('Plan Inline Editing', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: 'test-edit-plan',
				title: 'Test Edit Plan',
				goal: 'Original goal',
				context: 'Original context',
				approved: true,
				stage: 'approved'
			});

			await buildPlanRoutes(page, plan, [], []);
			await planDetailPage.goto('test-edit-plan');
		});

		test('shows Edit button for editable plan', async ({ planDetailPage }) => {
			await planDetailPage.expectPlanEditBtnVisible();
		});

		test('entering edit mode shows textareas with current values', async ({ planDetailPage }) => {
			await planDetailPage.clickPlanEdit();
			await planDetailPage.expectPlanEditMode();
		});

		test('cancel discards changes and exits edit mode', async ({ planDetailPage, page }) => {
			await planDetailPage.clickPlanEdit();
			await planDetailPage.editPlanGoal('Modified goal');
			await planDetailPage.cancelPlanEdit();
			await planDetailPage.expectPlanViewMode();
			// Original text should still be visible
			await expect(page.locator('.section-content').first()).toContainText('Original goal');
		});

		test('save persists changes via API', async ({ planDetailPage, page }) => {
			// Mock the PATCH endpoint
			let patchCalled = false;
			await page.route('**/workflow-api/plans/test-edit-plan', async (route) => {
				if (route.request().method() === 'PATCH') {
					patchCalled = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(
							mockPlan({
								slug: 'test-edit-plan',
								title: 'Test Edit Plan',
								goal: 'Updated goal',
								context: 'Updated context',
								approved: true,
								stage: 'approved'
							})
						)
					});
				} else {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(
							mockPlan({
								slug: 'test-edit-plan',
								title: 'Test Edit Plan',
								goal: 'Updated goal',
								context: 'Updated context',
								approved: true,
								stage: 'approved'
							})
						)
					});
				}
			});

			await planDetailPage.clickPlanEdit();
			await planDetailPage.editPlanGoal('Updated goal');
			await planDetailPage.editPlanContext('Updated context');
			await planDetailPage.savePlanEdit();

			// Should exit edit mode after save
			await planDetailPage.expectPlanViewMode();
			expect(patchCalled).toBe(true);
		});
	});

	test.describe('Plan Edit Button Visibility', () => {
		test('hides Edit button for executing plan', async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: 'executing-plan',
				title: 'Executing Plan',
				goal: 'Some goal',
				approved: true,
				stage: 'executing'
			});

			await buildPlanRoutes(page, plan, [], []);
			await planDetailPage.goto('executing-plan');
			await planDetailPage.expectPlanEditBtnHidden();
		});
	});
});
