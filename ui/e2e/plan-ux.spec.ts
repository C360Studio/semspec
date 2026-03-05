import { test, expect, mockPlan, mockPhase, mockTask } from './helpers/setup';
import type { Requirement } from '../src/lib/types/requirement';
import type { Scenario } from '../src/lib/types/scenario';

/**
 * Tests for Plan detail page UX:
 * - Resizable panels (PlanNavTree + DetailPanel)
 * - Tree navigation (requirements/scenarios - current UI)
 * - Tree navigation (phases/tasks - legacy plans, kept for backwards compat)
 * - Task approval workflow (via TaskDetail, legacy path)
 * - Plan inline editing
 *
 * The UI uses a tree navigation layout:
 *   Left panel: PlanNavTree (plan > requirements > scenarios)
 *   Right-top panel: PlanDetailPanel (PlanDetail | RequirementDetail | ScenarioDetail | TaskDetail)
 *   Right-bottom panel: ChatPanel
 *
 * ActionBar buttons in current UI:
 *   - "Approve Plan" (shown when plan is not approved and has a goal)
 *   - Cascade status (shown during auto-cascade after approval)
 *   - "Start Execution" (shown when plan is ready_for_execution)
 *   Removed: "Generate Phases", "Generate Tasks", "Approve All Tasks"
 */

// ============================================================================
// Mock data builders
// ============================================================================

function mockRequirement(overrides: {
	id: string;
	title: string;
	description?: string;
	status?: string;
}): Requirement {
	return {
		plan_id: 'plan-test',
		status: 'active',
		description: '',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		...overrides
	} as Requirement;
}

function mockScenario(overrides: {
	id: string;
	requirement_id: string;
	given: string;
	when: string;
	then: string[];
	status?: string;
}): Scenario {
	return {
		status: 'pending',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		...overrides
	} as Scenario;
}

// ============================================================================
// Route setup helpers
// ============================================================================

function buildPlanRoutes(
	page: import('@playwright/test').Page,
	plan: ReturnType<typeof mockPlan>,
	phases: ReturnType<typeof mockPhase>[],
	tasks: ReturnType<typeof mockTask>[],
	requirements: Requirement[] = [],
	scenarios: Scenario[] = []
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
		}),
		page.route(`**/workflow-api/plans/${plan.slug}/requirements`, (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(requirements)
			});
		}),
		page.route(`**/workflow-api/plans/${plan.slug}/scenarios**`, (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(scenarios)
			});
		})
	]);
}

// ============================================================================
// Tests
// ============================================================================

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

	test.describe('Requirement Navigation', () => {
		const requirementPlanSlug = 'test-req-plan';

		test.beforeEach(async ({ page, planDetailPage }) => {
			const plan = mockPlan({
				slug: requirementPlanSlug,
				title: 'Test Requirement Plan',
				goal: 'Build a user authentication system',
				approved: true,
				stage: 'ready_for_execution'
			});

			const requirements = [
				mockRequirement({
					id: 'req-1',
					title: 'User Login',
					description: 'Users must be able to log in with email and password',
					status: 'active'
				}),
				mockRequirement({
					id: 'req-2',
					title: 'Session Management',
					description: 'Sessions expire after 24 hours of inactivity',
					status: 'active'
				})
			];

			const scenarios = [
				mockScenario({
					id: 'sc-1',
					requirement_id: 'req-1',
					given: 'a registered user',
					when: 'they submit valid credentials',
					then: ['they are redirected to the dashboard', 'a session token is created'],
					status: 'pending'
				}),
				mockScenario({
					id: 'sc-2',
					requirement_id: 'req-1',
					given: 'a registered user',
					when: 'they submit an invalid password',
					then: ['an error message is displayed', 'no session is created'],
					status: 'pending'
				}),
				mockScenario({
					id: 'sc-3',
					requirement_id: 'req-2',
					given: 'an authenticated user',
					when: 'their session has been idle for 25 hours',
					then: ['they are automatically logged out', 'the session token is invalidated'],
					status: 'pending'
				})
			];

			await buildPlanRoutes(page, plan, [], [], requirements, scenarios);
			await planDetailPage.goto(requirementPlanSlug);
		});

		test('nav tree shows requirements when plan is approved and requirements exist', async ({
			planDetailPage,
			page
		}) => {
			await planDetailPage.expectNavTreeVisible();
			const reqNode1 = planDetailPage.navTree.locator('.tree-node.req-node', {
				hasText: 'User Login'
			});
			const reqNode2 = planDetailPage.navTree.locator('.tree-node.req-node', {
				hasText: 'Session Management'
			});
			await expect(reqNode1).toBeVisible();
			await expect(reqNode2).toBeVisible();
		});

		test('selecting a requirement shows RequirementDetail panel', async ({
			planDetailPage,
			page
		}) => {
			const reqNode = planDetailPage.navTree.locator('.tree-node.req-node', {
				hasText: 'User Login'
			});
			await reqNode.click();
			// RequirementDetail renders inside .detail-panel-container
			await expect(page.locator('.requirement-detail')).toBeVisible({ timeout: 5000 });
			await expect(page.locator('.requirement-detail .detail-title')).toContainText('User Login');
		});

		test('expanding a requirement shows its scenarios', async ({ planDetailPage, page }) => {
			// Click the expand button for "User Login" requirement
			const reqRow = planDetailPage.navTree.locator('.req-row').filter({ hasText: 'User Login' });
			const expandBtn = reqRow.locator('.expand-btn');
			await expandBtn.click();

			// Scenarios for req-1 should appear as scenario-node elements
			const scenarioNode1 = planDetailPage.navTree.locator('.tree-node.scenario-node').filter({
				hasText: 'valid credentials'
			});
			const scenarioNode2 = planDetailPage.navTree.locator('.tree-node.scenario-node').filter({
				hasText: 'invalid password'
			});
			await expect(scenarioNode1).toBeVisible({ timeout: 5000 });
			await expect(scenarioNode2).toBeVisible({ timeout: 5000 });
		});

		test('selecting a scenario navigates to it in the detail panel', async ({
			planDetailPage,
			page
		}) => {
			// First expand the requirement
			const reqRow = planDetailPage.navTree.locator('.req-row').filter({ hasText: 'User Login' });
			await reqRow.locator('.expand-btn').click();

			// Then click a scenario node
			const scenarioNode = planDetailPage.navTree.locator('.tree-node.scenario-node').filter({
				hasText: 'valid credentials'
			});
			await scenarioNode.click();

			// ScenarioDetail should appear in the detail panel
			await expect(page.locator('.scenario-detail')).toBeVisible({ timeout: 5000 });
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

	test.describe('ActionBar Cascade Behaviour', () => {
		test('shows cascade status when plan is approved and generating requirements', async ({
			page,
			planDetailPage
		}) => {
			const plan = mockPlan({
				slug: 'cascading-plan',
				title: 'Cascading Plan',
				goal: 'Some goal',
				approved: true,
				stage: 'approved'
			});

			await buildPlanRoutes(page, plan, [], [], []);
			await planDetailPage.goto('cascading-plan');

			// ActionBar should show cascade status (no Approve All button)
			await expect(page.locator('.action-bar .cascade-status')).toBeVisible();
			await expect(page.locator('.action-bar button', { hasText: 'Approve All' })).not.toBeVisible();
		});

		test('shows Start Execution button when plan is ready_for_execution', async ({
			page,
			planDetailPage
		}) => {
			const plan = mockPlan({
				slug: 'ready-plan',
				title: 'Ready Plan',
				goal: 'Some goal',
				approved: true,
				stage: 'ready_for_execution'
			});

			await buildPlanRoutes(page, plan, [], [], []);
			await planDetailPage.goto('ready-plan');

			await planDetailPage.expectExecuteBtnVisible();
			// Verify the removed buttons are absent
			await expect(page.locator('.action-bar button', { hasText: 'Generate Phases' })).not.toBeVisible();
			await expect(page.locator('.action-bar button', { hasText: 'Generate Tasks' })).not.toBeVisible();
			await expect(page.locator('.action-bar button', { hasText: 'Approve All' })).not.toBeVisible();
		});
	});
});
