import { test, expect, waitForHydration, seedInitializedProject, restoreWorkspace } from './helpers/setup';
import { MockLLMClient } from './helpers/mock-llm';
import { getPlans, waitForPlan, waitForPlanStage } from './helpers/workflow';

/**
 * Full UI Lifecycle E2E Test using Mock LLM.
 *
 * Exercises the complete Semspec UI as a user would:
 * plan creation → approval → task generation → execution → completion,
 * visiting every page and panel along the way.
 *
 * Uses the hello-world-code-execution mock scenario for deterministic
 * full-stack testing with real backend + mock LLM.
 *
 * Prerequisites:
 *   npm run test:e2e:lifecycle
 *   Or: MOCK_SCENARIO=hello-world-code-execution docker compose -f docker-compose.e2e.yml -f docker-compose.e2e-mock.yml up --wait
 */

const isUsingMockLLM = process.env.USE_MOCK_LLM === 'true';

test.describe('Full UI Lifecycle', () => {
	test.describe.configure({ mode: 'serial' });
	test.skip(!isUsingMockLLM, 'Skipping — USE_MOCK_LLM not set');

	let mockLLM: MockLLMClient;
	let planSlug: string;

	test.beforeAll(async () => {
		mockLLM = new MockLLMClient();
		await mockLLM.waitForHealthy(30000);
		await seedInitializedProject();
	});

	test.afterAll(async () => {
		await restoreWorkspace();
	});

	// ── Phase 1: Global Shell ──────────────────────────────────────────

	test('board page renders with empty state', async ({ boardPage }) => {
		await boardPage.goto();
		await boardPage.expectVisible();
		await boardPage.expectEmptyState();
	});

	test('sidebar is visible with correct nav items', async ({ page, sidebarPage }) => {
		await page.goto('/board');
		await waitForHydration(page);

		await sidebarPage.expectVisible();
		const items = await sidebarPage.getNavItems();
		expect(items).toEqual(['Board', 'Plans', 'Activity', 'Sources', 'Settings']);
	});

	test('system health indicator shows healthy', async ({ page, sidebarPage }) => {
		await page.goto('/board');
		await waitForHydration(page);
		await sidebarPage.expectHealthy();
	});

	// ── Phase 2: Plan Creation ─────────────────────────────────────────

	test('plans page shows plan mode indicator', async ({ page, chatPage }) => {
		await page.goto('/plans');
		await waitForHydration(page);

		await chatPage.openDrawer();
		await chatPage.expectMode('plan');
		await chatPage.expectModeLabel('Planning');
	});

	test('send plan description via chat', async ({ page, chatPage }) => {
		await page.goto('/plans');
		await waitForHydration(page);

		await chatPage.openDrawer();
		await chatPage.sendMessage('Build a hello world REST API with greeting endpoint');
		await chatPage.waitForResponse(45000);
		await chatPage.expectStatusMessage('Creating plan');
	});

	test('plan appears in API', async ({ page }) => {
		// Poll until at least one plan exists
		const start = Date.now();
		const timeout = 60000;

		while (Date.now() - start < timeout) {
			const plans = await getPlans(page);
			if (plans.length > 0) {
				planSlug = plans[0].slug;
				expect(planSlug).toBeTruthy();
				return;
			}
			await page.waitForTimeout(2000);
		}

		throw new Error('No plan created within timeout');
	});

	// ── Phase 3: Board with Plan ───────────────────────────────────────

	test('board shows plan card after creation', async ({ page, boardPage }) => {
		await boardPage.goto();
		await boardPage.expectVisible();

		// Wait for the plans grid to appear (plan may still be loading)
		await expect(async () => {
			await boardPage.expectNoEmptyState();
			await boardPage.expectPlansGrid();
		}).toPass({ timeout: 15000 });
	});

	// ── Phase 4: Plans List ────────────────────────────────────────────

	test('plans list shows plan row with slug', async ({ plansListPage }) => {
		await plansListPage.goto();
		await plansListPage.expectVisible();

		await expect(async () => {
			await plansListPage.expectPlanRowWithSlug(planSlug);
		}).toPass({ timeout: 15000 });
	});

	// ── Phase 5: Plan Detail (Draft) ───────────────────────────────────

	test('plan detail renders with split layout', async ({ planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		await planDetailPage.expectResizableSplitVisible();
		await planDetailPage.expectPlanPanelVisible();
		await planDetailPage.expectTasksPanelVisible();
	});

	test('action bar shows Approve Plan button', async ({ planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		await planDetailPage.expectActionBarVisible();
		await planDetailPage.expectApprovePlanBtnVisible();
	});

	// ── Phase 6: Approve → Generate Tasks ──────────────────────────────

	test('approve plan and wait for approved stage', async ({ page, planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		await planDetailPage.clickApprovePlan();

		// Wait for the plan to reach approved stage
		const plan = await waitForPlanStage(page, planSlug, 'approved', { timeout: 30000 });
		expect(plan).toBeTruthy();
		expect(plan!.approved).toBe(true);
	});

	test('generate tasks and wait for tasks to appear', async ({ page, planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();

		// Wait for Generate Tasks button to be visible after approval
		await expect(async () => {
			await planDetailPage.expectGenerateTasksBtnVisible();
		}).toPass({ timeout: 15000 });

		await planDetailPage.clickGenerateTasks();

		// Wait for tasks_generated stage
		const plan = await waitForPlanStage(page, planSlug, 'tasks_generated', { timeout: 60000 });
		expect(plan).toBeTruthy();
	});

	// ── Phase 7: Task Approval → Execution ─────────────────────────────

	test('approve all tasks', async ({ page, planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();

		// Wait for Approve All button
		await expect(async () => {
			await planDetailPage.expectApproveAllBtnVisible();
		}).toPass({ timeout: 15000 });

		await planDetailPage.clickApproveAll();

		// Wait for tasks_approved stage
		const plan = await waitForPlanStage(page, planSlug, 'tasks_approved', { timeout: 30000 });
		expect(plan).toBeTruthy();
	});

	test('start execution and verify pipeline', async ({ page, planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();

		// Wait for Start Execution button
		await expect(async () => {
			await planDetailPage.expectExecuteBtnVisible();
		}).toPass({ timeout: 15000 });

		await planDetailPage.clickExecute();

		// Wait for executing stage
		const plan = await waitForPlanStage(page, planSlug, 'executing', { timeout: 30000 });
		expect(plan).toBeTruthy();

		// Pipeline indicator should become visible
		await expect(async () => {
			await planDetailPage.expectPipelineVisible();
		}).toPass({ timeout: 15000 });
	});

	// ── Phase 8: Activity Page ─────────────────────────────────────────

	test('activity page renders with panels', async ({ activityPage }) => {
		await activityPage.goto();
		await activityPage.expectVisible();
		await activityPage.expectFeedPanelVisible();
		await activityPage.expectLoopsPanelVisible();
	});

	test('toggle to timeline view and back', async ({ activityPage }) => {
		await activityPage.goto();
		await activityPage.expectVisible();

		// Switch to timeline
		await activityPage.switchToTimeline();
		await activityPage.expectTimelineView();

		// Switch back to feed
		await activityPage.switchToFeed();
		await activityPage.expectFeedView();
	});

	// ── Phase 9: Wait for Completion ───────────────────────────────────

	test('wait for plan completion', async ({ page }) => {
		// Mock LLM should be fast — 120s is generous
		const plan = await waitForPlanStage(page, planSlug, 'complete', { timeout: 120000, interval: 3000 });
		expect(plan).toBeTruthy();
		expect(plan!.stage).toBe('complete');
	});

	// ── Phase 10: Sources Page ─────────────────────────────────────────

	test('sources page renders correctly', async ({ sourcesPage }) => {
		await sourcesPage.goto();
		await sourcesPage.expectVisible();
		await sourcesPage.expectHeaderText('Sources');
		await sourcesPage.expectSearchVisible();
		await sourcesPage.expectUploadBtnVisible();
	});

	// ── Phase 11: Entities Page ────────────────────────────────────────

	test('entities page renders correctly', async ({ entitiesPage }) => {
		await entitiesPage.goto();
		await entitiesPage.expectVisible();
		await entitiesPage.expectHeaderText('Entity Browser');
		await entitiesPage.expectSearchVisible();
		await entitiesPage.expectTypeFilterVisible();
	});

	// ── Phase 12: Settings Page ────────────────────────────────────────

	test('settings page renders all sections', async ({ settingsPage }) => {
		await settingsPage.goto();
		await settingsPage.expectVisible();
		await settingsPage.expectSections(3);
		await settingsPage.expectSectionTitles(['Appearance', 'Data & Storage', 'About']);
		await settingsPage.expectAboutVisible();
	});

	// ── Phase 13: Chat Drawer from Multiple Pages ──────────────────────

	test('chat drawer opens from board and shows correct mode per page', async ({ page, chatPage }) => {
		// Board page — chat mode
		await page.goto('/board');
		await waitForHydration(page);
		await chatPage.openDrawer();
		await chatPage.expectMode('chat');
		await chatPage.closeDrawer();

		// Plans page — plan mode
		await page.goto('/plans');
		await waitForHydration(page);
		await chatPage.openDrawer();
		await chatPage.expectMode('plan');
		await chatPage.closeDrawer();

		// Activity page — chat mode
		await page.goto('/activity');
		await waitForHydration(page);
		await chatPage.openDrawer();
		await chatPage.expectMode('chat');
		await chatPage.closeDrawer();
	});

	// ── Phase 14: Sidebar Navigation ───────────────────────────────────

	test('sidebar navigation walks through all pages', async ({ page, sidebarPage }) => {
		await page.goto('/board');
		await waitForHydration(page);

		// Board
		await sidebarPage.expectActivePage('Board');
		expect(page.url()).toContain('/board');

		// Plans
		await sidebarPage.navigateTo('Plans');
		await expect(page).toHaveURL(/\/plans/);
		await sidebarPage.expectActivePage('Plans');

		// Activity
		await sidebarPage.navigateTo('Activity');
		await expect(page).toHaveURL(/\/activity/);
		await sidebarPage.expectActivePage('Activity');

		// Sources
		await sidebarPage.navigateTo('Sources');
		await expect(page).toHaveURL(/\/sources/);
		await sidebarPage.expectActivePage('Sources');

		// Settings
		await sidebarPage.navigateTo('Settings');
		await expect(page).toHaveURL(/\/settings/);
		await sidebarPage.expectActivePage('Settings');
	});

	// ── Phase 15: Mock LLM Verification ────────────────────────────────

	test('mock LLM models were all called', async () => {
		const stats = await mockLLM.getStats();

		// Verify key models were invoked during the lifecycle
		expect(stats.total_calls).toBeGreaterThan(0);
		expect(stats.calls_by_model['mock-planner']).toBeGreaterThanOrEqual(1);
		expect(stats.calls_by_model['mock-reviewer']).toBeGreaterThanOrEqual(1);
		expect(stats.calls_by_model['mock-task-generator']).toBeGreaterThanOrEqual(1);
	});
});
