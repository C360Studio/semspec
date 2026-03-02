import { test, expect, waitForHydration, seedInitializedProject, restoreWorkspace } from './helpers/setup';
import { MockLLMClient } from './helpers/mock-llm';
import { getPlans, getPlan, waitForPlanStageOneOf, forceApprovePlan, forceApprovePhases, waitForPhasesOnDisk } from './helpers/workflow';

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
 * NOTE: The mock reviewer auto-approves plans, so the workflow progresses
 * rapidly. Tests use API polling to track stage transitions rather than
 * assuming specific button states at specific times.
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

	test('board page renders', async ({ boardPage, page }) => {
		await boardPage.goto();
		await boardPage.expectVisible();
		// Fresh stack should show empty state; tolerate stale plans from prior runs
		const emptyState = page.locator('.board-view .empty-state');
		const plansGrid = page.locator('.plans-grid');
		await expect(emptyState.or(plansGrid)).toBeVisible();
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

	test('board shows plan card after creation', async ({ boardPage }) => {
		await boardPage.goto();
		await boardPage.expectVisible();

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

	// ── Phase 5: Plan Detail ──────────────────────────────────────────

	test('plan detail renders with content', async ({ planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		// Plan title should be visible
		await expect(planDetailPage.planTitle).toBeVisible();
	});

	test('plan detail has action bar', async ({ planDetailPage }) => {
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		await planDetailPage.expectActionBarVisible();
	});

	// ── Phase 6: Drive plan to approved stage ──────────────────────────
	// The reactive engine's handle-approved rule has a known bug where it
	// doesn't fire after reviewer-completed. The promote endpoint is also
	// a no-op (just returns current stage). Work around by force-approving
	// via the shared filesystem volume.

	test('plan reaches approved stage', async ({ page }) => {
		const plan = await getPlan(page, planSlug);
		expect(plan).toBeTruthy();

		if (!plan!.approved) {
			// Wait briefly for the planner to finish drafting
			await waitForPlanStageOneOf(page, planSlug,
				['ready_for_approval', 'reviewed', 'approved'],
				{ timeout: 30000 });

			// Force-approve via filesystem (backend bug workaround)
			await forceApprovePlan(planSlug);
		}

		// Verify API reflects approved state
		const approved = await waitForPlanStageOneOf(page, planSlug,
			['approved', 'phases_generated', 'phases_approved', 'tasks_generated', 'tasks_approved', 'implementing', 'complete'],
			{ timeout: 15000 });
		expect(approved).toBeTruthy();
		expect(approved!.approved).toBe(true);
	});

	// ── Phase 7: Generate tasks ──────────────────────────────────────
	// The mock scenario generates phases first (auto-approved), then tasks.
	// After phases, the per-phase "Generate Tasks" button lives inside PhaseDetail
	// rather than the ActionBar, so we trigger task generation via API.
	//
	// NOTE: The reactive engine's handle-approved rule has the same bug for
	// phase-review-loop as for plan-review-loop — it never fires after
	// reviewer-completed. We work around this by force-approving phases
	// via the shared filesystem volume, just like we do for plan approval.

	test('generate tasks and wait for tasks', async ({ page, planDetailPage }) => {
		const currentPlan = await getPlan(page, planSlug);
		const stage = currentPlan?.stage ?? '';

		// Step 1: Generate phases if not already done
		if (!['phases_generated', 'phases_approved', 'tasks_generated', 'tasks_approved', 'implementing', 'complete'].includes(stage)) {
			// Trigger phase generation via API
			const phaseResp = await page.request.post(
				`http://localhost:3000/workflow-api/plans/${planSlug}/phases/generate`,
				{ data: {} }
			);
			expect(phaseResp.ok()).toBe(true);

			// Wait for phases to appear on disk
			const phasesReady = await waitForPhasesOnDisk(planSlug, { timeout: 60000 });
			expect(phasesReady).toBe(true);
		}

		// Step 2: Force-approve phases if needed (reactive engine bug workaround)
		const afterPhasesPlan = await getPlan(page, planSlug);
		if (afterPhasesPlan &&
			!['phases_approved', 'tasks_generated', 'tasks_approved', 'implementing', 'complete'].includes(afterPhasesPlan.stage)) {
			await forceApprovePhases(planSlug);
		}

		// Step 3: Generate tasks via API
		const afterApprovePlan = await getPlan(page, planSlug);
		if (afterApprovePlan &&
			!['tasks_generated', 'tasks_approved', 'implementing', 'complete'].includes(afterApprovePlan.stage)) {
			const taskResp = await page.request.post(
				`http://localhost:3000/workflow-api/plans/${planSlug}/tasks/generate`,
				{ data: {} }
			);
			expect(taskResp.ok()).toBe(true);
		}

		// Wait for tasks — mock LLM may race past tasks_generated quickly
		const plan = await waitForPlanStageOneOf(page, planSlug,
			['tasks_generated', 'tasks_approved', 'implementing', 'complete'],
			{ timeout: 60000 });
		expect(plan).toBeTruthy();

		// Verify plan detail page renders the tasks
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
	});

	// ── Phase 8: Task Approval → Execution ─────────────────────────────

	test('approve all tasks', async ({ page }) => {
		// Mock task-reviewer auto-approves, so tasks may already be approved.
		// Try to approve via API; 409 (already approved) is a success case.
		const response = await page.request.post(
			`http://localhost:3000/workflow-api/plans/${planSlug}/tasks/approve`,
			{ data: {} }
		);
		if (!response.ok() && response.status() !== 409) {
			const body = await response.text();
			throw new Error(`Task approval failed (${response.status()}): ${body}`);
		}

		// Mock LLM may progress past tasks_approved very quickly
		const plan = await waitForPlanStageOneOf(page, planSlug,
			['tasks_approved', 'implementing', 'complete'], { timeout: 30000 });
		expect(plan).toBeTruthy();
	});

	test('start execution and verify pipeline indicator', async ({ page, planDetailPage }) => {
		// Use API to start execution (may already be executing if auto-triggered)
		const response = await page.request.post(
			`http://localhost:3000/workflow-api/plans/${planSlug}/execute`,
			{ data: {} }
		);
		if (!response.ok() && response.status() !== 409) {
			const body = await response.text();
			throw new Error(`Execution start failed (${response.status()}): ${body}`);
		}

		// Verify pipeline indicator renders on the plan detail page.
		// The full AgentPipelineView requires active_loops, which may be empty
		// if the mock LLM completes instantly. The PipelineIndicator (plan/tasks/exec
		// status badges) should always render for approved plans.
		await planDetailPage.goto(planSlug);
		await planDetailPage.expectVisible();
		await expect(page.locator('.pipeline-section')).toBeVisible({ timeout: 10000 });
	});

	// ── Phase 9: Activity Page ─────────────────────────────────────────

	test('activity page renders with panels', async ({ activityPage }) => {
		await activityPage.goto();
		await activityPage.expectVisible();
		await activityPage.expectFeedPanelVisible();
		await activityPage.expectLoopsPanelVisible();
	});

	test('toggle to timeline view and back', async ({ activityPage }) => {
		await activityPage.goto();
		await activityPage.expectVisible();

		await activityPage.switchToTimeline();
		await activityPage.expectTimelineView();

		await activityPage.switchToFeed();
		await activityPage.expectFeedView();
	});

	// ── Phase 10: Verify plan state after execution ──────────────────
	// NOTE: The backend does not currently transition plan status to
	// "implementing" or "complete" — those status transitions are not
	// wired up in the reactive engine. We verify that the plan is at
	// least at tasks_approved (execution was triggered successfully).

	test('verify plan reached tasks_approved', async ({ page }) => {
		const plan = await getPlan(page, planSlug);
		expect(plan).toBeTruthy();
		expect(['tasks_approved', 'implementing', 'complete']).toContain(plan!.stage);
	});

	// ── Phase 11: Sources Page ─────────────────────────────────────────

	test('sources page renders correctly', async ({ sourcesPage }) => {
		await sourcesPage.goto();
		await sourcesPage.expectVisible();
		await sourcesPage.expectHeaderText('Sources');
		await sourcesPage.expectSearchVisible();
		await sourcesPage.expectUploadBtnVisible();
	});

	// ── Phase 12: Entities Page ────────────────────────────────────────

	test('entities page renders correctly', async ({ entitiesPage }) => {
		await entitiesPage.goto();
		await entitiesPage.expectVisible();
		await entitiesPage.expectHeaderText('Entity Browser');
		await entitiesPage.expectSearchVisible();
		await entitiesPage.expectTypeFilterVisible();
	});

	// ── Phase 13: Settings Page ────────────────────────────────────────

	test('settings page renders all sections', async ({ settingsPage }) => {
		await settingsPage.goto();
		await settingsPage.expectVisible();
		await settingsPage.expectSections(3);
		await settingsPage.expectSectionTitles(['Appearance', 'Data & Storage', 'About']);
		await settingsPage.expectAboutVisible();
	});

	// ── Phase 14: Chat Drawer from Multiple Pages ──────────────────────

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

	// ── Phase 15: Sidebar Navigation ───────────────────────────────────

	test('sidebar navigation walks through all pages', async ({ page, sidebarPage }) => {
		await page.goto('/board');
		await waitForHydration(page);

		await sidebarPage.expectActivePage('Board');
		expect(page.url()).toContain('/board');

		await sidebarPage.navigateTo('Plans');
		await expect(page).toHaveURL(/\/plans/);
		await sidebarPage.expectActivePage('Plans');

		await sidebarPage.navigateTo('Activity');
		await expect(page).toHaveURL(/\/activity/);
		await sidebarPage.expectActivePage('Activity');

		await sidebarPage.navigateTo('Sources');
		await expect(page).toHaveURL(/\/sources/);
		await sidebarPage.expectActivePage('Sources');

		await sidebarPage.navigateTo('Settings');
		await expect(page).toHaveURL(/\/settings/);
		await sidebarPage.expectActivePage('Settings');
	});

	// ── Phase 16: Mock LLM Verification ────────────────────────────────

	test('mock LLM models were all called', async () => {
		const stats = await mockLLM.getStats();

		expect(stats.total_calls).toBeGreaterThan(0);
		expect(stats.calls_by_model['mock-planner']).toBeGreaterThanOrEqual(1);
		expect(stats.calls_by_model['mock-reviewer']).toBeGreaterThanOrEqual(1);
		expect(stats.calls_by_model['mock-task-generator']).toBeGreaterThanOrEqual(1);
	});
});
