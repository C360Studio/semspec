import { test, expect, testData } from './helpers/setup';

/**
 * Tests for Plan detail page UX improvements:
 * - Collapsible panels (Plan, Tasks)
 * - ActionBar buttons (Approve Plan, Generate Tasks, Approve All, Execute)
 * - DataTable features (filtering, sorting, pagination, expandable rows)
 * - Task approval workflow
 */

test.describe('Plan Detail UX', () => {
	test.describe('Resizable Panels', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			// Mock a plan with tasks
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-panels-plan',
							title: 'Test Panels Plan',
							goal: 'Test resizable panels',
							approved: true,
							stage: 'tasks',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/test-panels-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-panels-plan',
						title: 'Test Panels Plan',
						goal: 'Test resizable panels',
						approved: true,
						stage: 'tasks',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-panels-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{ id: 'task-1', sequence: 1, description: 'First task', status: 'pending', type: 'implement' },
						{ id: 'task-2', sequence: 2, description: 'Second task', status: 'pending_approval', type: 'test' },
						{ id: 'task-3', sequence: 3, description: 'Third task', status: 'approved', type: 'document' }
					])
				});
			});

			await planDetailPage.goto('test-panels-plan');
		});

		test('shows resizable split layout with Plan and Tasks panels', async ({ planDetailPage }) => {
			await planDetailPage.expectResizableSplitVisible();
			await planDetailPage.expectPlanPanelVisible();
			await planDetailPage.expectTasksPanelVisible();
		});

		test('shows resize divider between panels', async ({ planDetailPage }) => {
			await planDetailPage.expectResizableSplitVisible();
			await planDetailPage.expectResizeDividerVisible();
		});
	});

	test.describe('Task DataTable', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			// Mock a plan with many tasks for pagination testing
			const tasks = [];
			for (let i = 1; i <= 30; i++) {
				tasks.push({
					id: `task-${i}`,
					sequence: i,
					description: `Task ${i}: Implement feature ${i}`,
					status: i <= 10 ? 'pending' : i <= 20 ? 'pending_approval' : 'approved',
					type: i % 3 === 0 ? 'test' : i % 2 === 0 ? 'document' : 'implement'
				});
			}

			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-datatable-plan',
							title: 'Test DataTable Plan',
							approved: true,
							stage: 'tasks',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/test-datatable-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-datatable-plan',
						title: 'Test DataTable Plan',
						approved: true,
						stage: 'tasks',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-datatable-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(tasks)
				});
			});

			await planDetailPage.goto('test-datatable-plan');
		});

		test('displays task table with tasks', async ({ planDetailPage }) => {
			await planDetailPage.expectTaskTableVisible();
			await planDetailPage.expectTaskTableCount('30 tasks');
		});

		test('filters tasks by text search', async ({ planDetailPage, page }) => {
			await planDetailPage.filterTasks('feature 1');
			// Should show only tasks with "feature 1" in description
			// Wait for filter to apply
			await page.waitForTimeout(100);
			// Count should update to show filtered results
			const countLabel = page.locator('[data-testid="task-list-count"]');
			await expect(countLabel).toContainText('of 30');
		});

		test('filters tasks by status', async ({ planDetailPage, page }) => {
			await planDetailPage.filterTasksByStatus('pending_approval');
			await page.waitForTimeout(100);
			// Should show 10 tasks with pending_approval status (tasks 11-20)
			const countLabel = page.locator('[data-testid="task-list-count"]');
			await expect(countLabel).toContainText('10 of 30');
		});

		test('paginates tasks (20 per page)', async ({ planDetailPage }) => {
			// First page should show first 20 tasks
			await planDetailPage.expectCurrentPage(1, 2);
			// Navigate to page 2
			await planDetailPage.goToPage(2);
			await planDetailPage.expectCurrentPage(2, 2);
		});

		test('expands task row to show details', async ({ planDetailPage }) => {
			await planDetailPage.expandTaskRow(0);
			await planDetailPage.expectTaskRowExpanded(0);
		});
	});

	test.describe('Task Approval', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-approval-plan',
							title: 'Test Approval Plan',
							approved: true,
							stage: 'tasks_generated',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/test-approval-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-approval-plan',
						title: 'Test Approval Plan',
						approved: true,
						stage: 'tasks_generated',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-approval-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							id: 'task-1',
							sequence: 1,
							description: 'Implement authentication',
							status: 'pending_approval',
							type: 'implement',
							acceptance_criteria: [
								{ given: 'a user with valid credentials', when: 'they log in', then: 'they should see the dashboard' }
							]
						},
						{
							id: 'task-2',
							sequence: 2,
							description: 'Write unit tests',
							status: 'pending_approval',
							type: 'test'
						}
					])
				});
			});

			await planDetailPage.goto('test-approval-plan');
		});

		test('shows approve and reject buttons for pending_approval tasks', async ({ page }) => {
			// Look for action buttons in the task rows
			const approveBtn = page.locator('[data-testid="task-list-row"]').first().locator('.btn-success');
			const rejectBtn = page.locator('[data-testid="task-list-row"]').first().locator('.btn-outline');

			await expect(approveBtn).toBeVisible();
			await expect(rejectBtn).toBeVisible();
		});

		test('shows Approve All button when tasks pending approval', async ({ planDetailPage }) => {
			// Wait for ActionBar to be visible first
			await planDetailPage.expectActionBarVisible();
			await planDetailPage.expectApproveAllBtnVisible();
		});

		test('can approve a task', async ({ page, planDetailPage }) => {
			// Mock the approve endpoint
			await page.route('**/workflow-api/plans/test-approval-plan/tasks/task-1/approve', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						id: 'task-1',
						sequence: 1,
						description: 'Implement authentication',
						status: 'approved',
						type: 'implement'
					})
				});
			});

			await planDetailPage.approveTask(0);

			// After approval, the status should change
			// This depends on how the UI updates - may need to wait for a reload
		});

		test('can reject a task with reason', async ({ page, planDetailPage }) => {
			// Mock the reject endpoint
			await page.route('**/workflow-api/plans/test-approval-plan/tasks/task-1/reject', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						id: 'task-1',
						sequence: 1,
						description: 'Implement authentication',
						status: 'rejected',
						type: 'implement',
						rejection_reason: 'Needs more detail'
					})
				});
			});

			await planDetailPage.rejectTask(0, 'Needs more detail');
		});

		test('expands task to show acceptance criteria', async ({ planDetailPage, page }) => {
			await planDetailPage.expandTaskRow(0);
			await planDetailPage.expectTaskRowExpanded(0);

			// Check that acceptance criteria is visible (use first() to avoid strict mode violation)
			const acSection = page.locator('.acceptance-criteria').first();
			await expect(acSection).toBeVisible();
			await expect(acSection).toContainText('Given');
			await expect(acSection).toContainText('When');
			await expect(acSection).toContainText('Then');
		});
	});

	test.describe('Plan Inline Editing', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-edit-plan',
							title: 'Test Edit Plan',
							goal: 'Original goal',
							context: 'Original context',
							approved: true,
							stage: 'approved',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/test-edit-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-edit-plan',
						title: 'Test Edit Plan',
						goal: 'Original goal',
						context: 'Original context',
						approved: true,
						stage: 'approved',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-edit-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

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
			await page.route('**/workflow-api/plans/test-edit-plan', async route => {
				if (route.request().method() === 'PATCH') {
					patchCalled = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							slug: 'test-edit-plan',
							title: 'Test Edit Plan',
							goal: 'Updated goal',
							context: 'Updated context',
							approved: true,
							stage: 'approved',
							active_loops: []
						})
					});
				} else {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							slug: 'test-edit-plan',
							title: 'Test Edit Plan',
							goal: 'Updated goal',
							context: 'Updated context',
							approved: true,
							stage: 'approved',
							active_loops: []
						})
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
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([{
						slug: 'executing-plan',
						title: 'Executing Plan',
						goal: 'Some goal',
						approved: true,
						stage: 'executing',
						active_loops: []
					}])
				});
			});

			await page.route('**/workflow-api/plans/executing-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'executing-plan',
						title: 'Executing Plan',
						goal: 'Some goal',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/executing-plan/tasks', route => {
				route.fulfill({ status: 200, body: JSON.stringify([]) });
			});

			await planDetailPage.goto('executing-plan');
			await planDetailPage.expectPlanEditBtnHidden();
		});
	});

	test.describe('Task Creation', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([{
						slug: 'test-task-crud',
						title: 'Test Task CRUD',
						goal: 'Test task creation',
						approved: true,
						stage: 'approved',
						active_loops: []
					}])
				});
			});

			await page.route('**/workflow-api/plans/test-task-crud', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-task-crud',
						title: 'Test Task CRUD',
						goal: 'Test task creation',
						approved: true,
						stage: 'approved',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-task-crud/tasks', route => {
				if (route.request().method() === 'GET') {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{ id: 'task-1', sequence: 1, description: 'Existing task', status: 'pending', type: 'implement' }
						])
					});
				} else {
					route.continue();
				}
			});

			await planDetailPage.goto('test-task-crud');
		});

		test('shows Add Task button for approved plan', async ({ planDetailPage }) => {
			await planDetailPage.expectAddTaskBtnVisible();
		});

		test('clicking Add Task opens modal', async ({ planDetailPage }) => {
			await planDetailPage.clickAddTask();
			await planDetailPage.expectTaskModalVisible();
		});

		test('modal can be cancelled', async ({ planDetailPage }) => {
			await planDetailPage.clickAddTask();
			await planDetailPage.expectTaskModalVisible();
			await planDetailPage.cancelTaskModal();
			await planDetailPage.expectTaskModalHidden();
		});

		test('creating task calls API and closes modal', async ({ planDetailPage, page }) => {
			let postCalled = false;
			await page.route('**/workflow-api/plans/test-task-crud/tasks', async route => {
				if (route.request().method() === 'POST') {
					postCalled = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							id: 'task-new',
							sequence: 2,
							description: 'New task description',
							status: 'pending',
							type: 'implement'
						})
					});
				} else {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{ id: 'task-1', sequence: 1, description: 'Existing task', status: 'pending', type: 'implement' },
							{ id: 'task-new', sequence: 2, description: 'New task description', status: 'pending', type: 'implement' }
						])
					});
				}
			});

			await planDetailPage.clickAddTask();
			await planDetailPage.fillTaskDescription('New task description');
			await planDetailPage.selectTaskType('implement');
			await planDetailPage.saveTaskModal();

			await planDetailPage.expectTaskModalHidden();
			expect(postCalled).toBe(true);
		});
	});

	test.describe('Task Editing', () => {
		test.beforeEach(async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([{
						slug: 'test-task-edit',
						title: 'Test Task Edit',
						approved: true,
						stage: 'approved',
						active_loops: []
					}])
				});
			});

			await page.route('**/workflow-api/plans/test-task-edit', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-task-edit',
						title: 'Test Task Edit',
						approved: true,
						stage: 'approved',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/test-task-edit/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							id: 'task-1',
							sequence: 1,
							description: 'Editable task',
							status: 'pending',
							type: 'implement',
							files: ['src/main.ts']
						}
					])
				});
			});

			await planDetailPage.goto('test-task-edit');
		});

		test('clicking edit button opens modal with task data', async ({ planDetailPage, page }) => {
			await planDetailPage.editTask(0);
			await planDetailPage.expectTaskModalVisible();
			// Modal should have existing task data
			await expect(page.locator('#task-description')).toHaveValue('Editable task');
		});

		test('editing task calls PATCH API', async ({ planDetailPage, page }) => {
			let patchCalled = false;
			await page.route('**/workflow-api/plans/test-task-edit/tasks/task-1', async route => {
				if (route.request().method() === 'PATCH') {
					patchCalled = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							id: 'task-1',
							sequence: 1,
							description: 'Updated description',
							status: 'pending',
							type: 'test'
						})
					});
				} else {
					route.continue();
				}
			});

			await planDetailPage.editTask(0);
			await planDetailPage.fillTaskDescription('Updated description');
			await planDetailPage.selectTaskType('test');
			await planDetailPage.saveTaskModal();

			await planDetailPage.expectTaskModalHidden();
			expect(patchCalled).toBe(true);
		});
	});
});
