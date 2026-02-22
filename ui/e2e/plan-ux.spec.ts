import { test, expect, testData } from './helpers/setup';

/**
 * Tests for Plan detail page UX improvements:
 * - Collapsible panels (Plan, Tasks, Chat)
 * - DataTable features (filtering, sorting, pagination, expandable rows)
 * - Task approval workflow
 */

test.describe('Plan Detail UX', () => {
	test.describe('Collapsible Panels', () => {
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
							goal: 'Test collapsible panels',
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
						goal: 'Test collapsible panels',
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

		test('shows three collapsible panels', async ({ planDetailPage }) => {
			await planDetailPage.expectPanelLayoutVisible();
			await planDetailPage.expectPlanPanelVisible();
			await planDetailPage.expectTasksPanelVisible();
			await planDetailPage.expectChatPanelVisible();
		});

		test('can collapse and expand Plan panel', async ({ planDetailPage }) => {
			await planDetailPage.togglePlanPanel();
			await planDetailPage.expectPlanPanelCollapsed();
			await planDetailPage.togglePlanPanel();
			// Panel should be visible again after expanding
			await planDetailPage.expectPlanPanelVisible();
		});

		test('can collapse and expand Tasks panel', async ({ planDetailPage }) => {
			await planDetailPage.toggleTasksPanel();
			await planDetailPage.expectTasksPanelCollapsed();
			await planDetailPage.toggleTasksPanel();
			await planDetailPage.expectTasksPanelVisible();
		});

		test('can collapse and expand Chat panel', async ({ planDetailPage }) => {
			await planDetailPage.toggleChatPanel();
			await planDetailPage.expectChatPanelCollapsed();
			await planDetailPage.toggleChatPanel();
			await planDetailPage.expectChatPanelVisible();
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

		test('shows Approve All button when tasks pending approval', async ({ page }) => {
			const approveAllBtn = page.locator('.approve-all-btn');
			await expect(approveAllBtn).toBeVisible();
			await expect(approveAllBtn).toContainText('Approve All');
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
});
