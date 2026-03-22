import { test, expect, mockPlan } from './helpers/setup';
import type { Requirement } from '../src/lib/types/requirement';
import type { Scenario } from '../src/lib/types/scenario';

/**
 * Tests for the health-check plan lifecycle in the UI:
 * - Plan creation shows in board/list
 * - Plan detail page renders goal and context
 * - Requirements appear after approval + auto-cascade
 * - Scenarios appear under requirements
 * - Pipeline shows active during execution
 * - Pipeline shows reviewing_rollup status
 * - Plan completes with success state
 *
 * Uses mock API routes — no real backend needed.
 * Run with: npx playwright test health-check-pipeline.spec.ts
 */

// ============================================================================
// Mock data
// ============================================================================

const healthCheckPlan = mockPlan({
	slug: 'health-check',
	goal: 'Add a /health endpoint that returns JSON with status, uptime, and Go version',
	context: 'Simple Go HTTP service needs health monitoring for production readiness',
	approved: true,
	stage: 'implementing'
});

const requirement: Requirement = {
	id: 'req-health-1',
	plan_id: 'plan-health-check',
	title: 'Health endpoint returns service status',
	description: 'The /health endpoint must return a JSON response with status, uptime, and version fields',
	status: 'active',
	created_at: new Date().toISOString(),
	updated_at: new Date().toISOString()
} as Requirement;

const scenarios: Scenario[] = [
	{
		id: 'sc-health-1',
		requirement_id: 'req-health-1',
		given: 'the server is running and healthy',
		when: 'a GET request is made to /health',
		then: [
			'the response status is 200',
			'the body contains status: ok',
			'the body contains uptime as a number'
		],
		status: 'completed',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString()
	} as Scenario,
	{
		id: 'sc-health-2',
		requirement_id: 'req-health-1',
		given: 'the server just started',
		when: 'a GET request is made to /health',
		then: ['uptime is close to zero', 'version matches runtime.Version()'],
		status: 'pending',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString()
	} as Scenario
];

// ============================================================================
// Route setup helpers
// ============================================================================

/**
 * Register default empty responses for all sub-resource endpoints that every
 * plan detail page fetches on load. Individual tests override these as needed.
 */
async function setupDefaultSubRoutes(page: import('@playwright/test').Page): Promise<void> {
	await page.route('**/workflow-api/plans/*/phases', route => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});
	await page.route('**/workflow-api/plans/*/requirements', route => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});
	await page.route('**/workflow-api/plans/*/scenarios**', route => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});
	await page.route('**/workflow-api/plans/*/tasks', route => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});
}

function withStage(stage: string, extraOverrides: object = {}) {
	return { ...healthCheckPlan, stage, ...extraOverrides };
}

// ============================================================================
// Tests
// ============================================================================

test.describe('Health-Check Plan Pipeline Lifecycle', () => {
	test.beforeEach(async ({ page }) => {
		await setupDefaultSubRoutes(page);
	});

	test('plan board shows health-check plan card with goal text', async ({ page, boardPage }) => {
		const plan = withStage('approved');

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await boardPage.goto();
		await boardPage.expectVisible();
		await boardPage.expectPlansGrid();

		// The plan card must be present and display the goal text
		const planCard = page.locator('.plan-card', { hasText: healthCheckPlan.goal! });
		await expect(planCard).toBeVisible();
	});

	test('plan detail page renders goal and context', async ({ page, planDetailPage }) => {
		const plan = withStage('approved');

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await planDetailPage.goto('health-check');
		await expect(planDetailPage.planDetail).toBeVisible();

		// Goal and context must appear in the rendered detail
		await expect(page.locator('.plan-detail')).toContainText(healthCheckPlan.goal!);
		await expect(page.locator('.plan-detail')).toContainText(healthCheckPlan.context!);
	});

	test('requirements appear in nav tree after approval and auto-cascade', async ({
		page,
		planDetailPage
	}) => {
		const plan = withStage('requirements_generated');

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		// Override the default empty requirements route with real data
		await page.route('**/workflow-api/plans/health-check/requirements', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([requirement])
			});
		});

		await planDetailPage.goto('health-check');
		await planDetailPage.expectNavTreeVisible();

		// The requirement title must appear as a tree node
		await planDetailPage.expectRequirementInTree(requirement.title);
	});

	test('scenarios appear under requirement in nav tree', async ({ page, planDetailPage }) => {
		const plan = withStage('scenarios_generated');

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await page.route('**/workflow-api/plans/health-check/requirements', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([requirement])
			});
		});

		await page.route('**/workflow-api/plans/health-check/scenarios**', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(scenarios)
			});
		});

		await planDetailPage.goto('health-check');
		await planDetailPage.expectNavTreeVisible();

		// Expand the requirement to reveal its scenarios
		await planDetailPage.expandRequirementInTree(requirement.title);

		// Both scenario nodes must be present (matched by their "when" text)
		await planDetailPage.expectScenarioInTree('GET request is made to /health');
		// Second scenario shares the same "when" — verify at least one appears
		const scenarioNodes = planDetailPage.navTree.locator('.tree-node.scenario-node');
		await expect(scenarioNodes).toHaveCount(scenarios.length);
	});

	test('execution stage shows active pipeline with spinner', async ({ page, planDetailPage }) => {
		const plan = withStage('implementing', {
			active_loops: [
				{
					loop_id: 'health-check-builder-loop',
					role: 'builder',
					model: 'claude-3-sonnet',
					state: 'executing',
					iterations: 2,
					max_iterations: 10
				}
			]
		});

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await planDetailPage.goto('health-check');
		await planDetailPage.expectPipelineVisible();

		// An active stage with a spinner must be present during execution
		const activeStage = page.locator('.pipeline-stage.active');
		await expect(activeStage).toBeVisible();

		const spinner = activeStage.locator('.spin');
		await expect(spinner).toBeVisible();
	});

	test('reviewing_rollup stage shows correct rollup status badge', async ({
		page,
		planDetailPage
	}) => {
		const plan = withStage('reviewing_rollup', {
			active_loops: [
				{
					loop_id: 'health-check-rollup-loop',
					role: 'reviewer',
					model: 'claude-3-sonnet',
					state: 'executing',
					iterations: 1,
					max_iterations: 5
				}
			]
		});

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await planDetailPage.goto('health-check');

		// Stage badge must reflect reviewing_rollup
		const stageBadge = page.locator('.plan-stage');
		await expect(stageBadge).toBeVisible();
		const badgeText = await stageBadge.textContent();
		expect(badgeText?.toLowerCase()).toMatch(/reviewing|rollup/i);

		// Pipeline must remain visible — rollup is still within the execute phase
		await planDetailPage.expectPipelineVisible();
		await expect(planDetailPage.agentPipelineView).toBeVisible();
	});

	test('reviewing_rollup stage shows active loop in pipeline', async ({
		page,
		planDetailPage
	}) => {
		const plan = withStage('reviewing_rollup', {
			active_loops: [
				{
					loop_id: 'health-check-rollup-active',
					role: 'reviewer',
					model: 'claude-3-sonnet',
					state: 'executing',
					iterations: 1,
					max_iterations: 5
				}
			]
		});

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await planDetailPage.goto('health-check');
		await planDetailPage.expectPipelineVisible();

		// The active rollup loop produces an active pipeline stage with a spinner
		const activeStage = page.locator('.pipeline-stage.active');
		await expect(activeStage).toBeVisible();

		const spinner = activeStage.locator('.spin');
		await expect(spinner).toBeVisible();
	});

	test('complete plan shows success stage badge and no active pipeline stages', async ({
		page,
		planDetailPage
	}) => {
		const plan = withStage('complete', { active_loops: [] });

		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/health-check', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await planDetailPage.goto('health-check');

		// The stage badge must read "Complete"
		const stageBadge = page.locator('.plan-stage');
		await expect(stageBadge).toHaveText('Complete');

		// No active pipeline stages should remain
		const activeStages = page.locator('.pipeline-stage.active');
		await expect(activeStages).toHaveCount(0);
	});
});
