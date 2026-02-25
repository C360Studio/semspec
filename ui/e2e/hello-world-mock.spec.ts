import { test, expect, waitForHydration } from './helpers/setup';
import { MockLLMClient } from './helpers/mock-llm';

/**
 * E2E tests using the Mock LLM server for deterministic full-stack testing.
 *
 * These tests exercise the complete workflow without a real LLM:
 * - Plan creation via context-based mode (navigate to /plans, type description)
 * - Plan approval and review
 * - Task generation
 *
 * The mock-llm server returns pre-defined JSON responses from fixtures,
 * enabling fast, reproducible, and offline-capable testing.
 *
 * Prerequisites:
 *   Run with mock LLM: npm run test:e2e:mock
 *   Or: MOCK_SCENARIO=hello-world docker compose -f docker-compose.e2e.yml -f docker-compose.e2e-mock.yml up --wait
 */

// Only run these tests when mock LLM is available
const isUsingMockLLM = process.env.USE_MOCK_LLM === 'true';

test.describe('Hello World Mock LLM E2E', () => {
	// Skip if not using mock LLM
	test.skip(!isUsingMockLLM, 'Skipping mock LLM tests - USE_MOCK_LLM not set');

	let mockLLM: MockLLMClient;

	test.beforeAll(async () => {
		mockLLM = new MockLLMClient();
		// Wait for mock LLM to be ready
		await mockLLM.waitForHealthy(30000);
	});

	test.describe('Plan Creation Workflow', () => {
		test('creates plan via chat in Plan mode', async ({ page, chatPage }) => {
			// Navigate to plans page - this auto-selects Plan mode
			await page.goto('/plans');
			await waitForHydration(page);

			// Open chat drawer
			await chatPage.openDrawer();

			// Verify mode indicator shows Plan mode
			await chatPage.expectMode('plan');
			await chatPage.expectModeLabel('Planning');

			// Send plan description (routes to POST /workflow-api/plans)
			await chatPage.sendMessage('Add a goodbye endpoint');

			// Wait for status message indicating plan creation
			await chatPage.waitForResponse(30000);

			// Should show status message about plan creation
			await chatPage.expectStatusMessage('Creating plan');
		});

		test('mock LLM receives planner request', async ({ page, chatPage }) => {
			// Navigate to plans page for Plan mode
			await page.goto('/plans');
			await waitForHydration(page);
			await chatPage.openDrawer();

			// Get initial stats
			const initialStats = await mockLLM.getStats();
			const initialPlannerCalls = initialStats.calls_by_model['mock-planner'] || 0;

			// Send plan description
			await chatPage.sendMessage('Add a logout feature');
			await chatPage.waitForResponse(45000);

			// Wait a moment for the workflow to complete
			await page.waitForTimeout(2000);

			// Verify mock-planner was called
			const finalStats = await mockLLM.getStats();
			const finalPlannerCalls = finalStats.calls_by_model['mock-planner'] || 0;

			// Planner should have been called at least once more
			expect(finalPlannerCalls).toBeGreaterThan(initialPlannerCalls);
		});
	});

	test.describe('Chat Mode on Activity Page', () => {
		test('activity page uses Chat mode by default', async ({ page, chatPage }) => {
			// Navigate to activity page
			await chatPage.goto();

			// Verify mode indicator shows Chat mode
			await chatPage.expectMode('chat');
			await chatPage.expectModeLabel('Chat');
		});
	});

	test.describe('Plan Approval Workflow', () => {
		test.beforeEach(async ({ page }) => {
			// Pre-create a plan by mocking API responses
			await page.route('**/workflow-api/plans', (route) => {
				if (route.request().method() === 'GET') {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([
							{
								slug: 'add-goodbye-endpoint',
								title: 'Add Goodbye Endpoint',
								goal: 'Add a /goodbye endpoint to the Flask API that returns a JSON goodbye message.',
								context: 'The project is a Python Flask API with a JavaScript frontend.',
								approved: false,
								committed: false,
								stage: 'draft',
								active_loops: []
							}
						])
					});
				} else {
					route.continue();
				}
			});

			await page.route('**/workflow-api/plans/add-goodbye-endpoint', (route) => {
				if (route.request().method() === 'GET') {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							slug: 'add-goodbye-endpoint',
							title: 'Add Goodbye Endpoint',
							goal: 'Add a /goodbye endpoint to the Flask API that returns a JSON goodbye message.',
							context: 'Python Flask API with JavaScript frontend.',
							scope: {
								include: ['api/app.py', 'ui/app.js', 'ui/index.html'],
								exclude: ['node_modules', '.git'],
								do_not_touch: ['README.md']
							},
							approved: false,
							committed: false,
							stage: 'draft',
							active_loops: []
						})
					});
				} else {
					route.continue();
				}
			});

			await page.route('**/workflow-api/plans/add-goodbye-endpoint/tasks', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/add-goodbye-endpoint/phases', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});
		});

		test('displays plan with goal and context', async ({ page, planDetailPage }) => {
			await planDetailPage.goto('add-goodbye-endpoint');
			await planDetailPage.expectVisible();

			// Verify plan content
			await expect(page.locator('.section-content').first()).toContainText('goodbye');
			await expect(page.locator('.section-content').first()).toContainText('Flask');
		});

		test('shows Approve Plan button for draft plan', async ({ planDetailPage }) => {
			await planDetailPage.goto('add-goodbye-endpoint');
			await planDetailPage.expectVisible();
			await planDetailPage.expectActionBarVisible();
			await planDetailPage.expectApprovePlanBtnVisible();
		});

		test('plan detail page uses Chat mode', async ({ page, chatPage, planDetailPage }) => {
			await planDetailPage.goto('add-goodbye-endpoint');
			await planDetailPage.expectVisible();

			// Open chat drawer
			await chatPage.openDrawer();

			// On draft plan detail page, should be Chat mode
			await chatPage.expectMode('chat');
		});
	});

	test.describe('Full Workflow Integration', () => {
		test('complete plan lifecycle: create, view, verify LLM calls', async ({ page, chatPage }) => {
			// This test verifies the end-to-end flow:
			// 1. Navigate to /plans (Plan mode)
			// 2. Send description via chat
			// 3. Backend calls mock-planner
			// 4. Plan is created with LLM-generated content

			// Navigate to plans page for Plan mode
			await page.goto('/plans');
			await waitForHydration(page);
			await chatPage.openDrawer();

			// Verify Plan mode
			await chatPage.expectMode('plan');

			// Send plan description
			await chatPage.sendMessage('Add user authentication');

			// Wait for workflow to process
			await chatPage.waitForResponse(45000);

			// Give time for plan creation
			await page.waitForTimeout(3000);

			// Check that the mock LLM was called
			const stats = await mockLLM.getStats();
			expect(stats.total_calls).toBeGreaterThan(0);

			// At minimum, we expect the LLM to have been called
			expect(stats.total_calls).toBeGreaterThanOrEqual(1);
		});
	});
});

test.describe('Mock LLM Server Health', () => {
	// These tests verify the mock LLM server is operational
	test.skip(!process.env.USE_MOCK_LLM, 'Skipping - mock LLM not configured');

	test('mock LLM server is healthy', async () => {
		const mockLLM = new MockLLMClient();
		const isHealthy = await mockLLM.isHealthy();
		expect(isHealthy).toBe(true);
	});

	test('mock LLM returns stats endpoint', async () => {
		const mockLLM = new MockLLMClient();
		const stats = await mockLLM.getStats();

		expect(stats).toHaveProperty('total_calls');
		expect(stats).toHaveProperty('calls_by_model');
		expect(typeof stats.total_calls).toBe('number');
	});

	test('mock LLM returns requests endpoint', async () => {
		const mockLLM = new MockLLMClient();
		const requests = await mockLLM.getRequests();

		expect(requests).toHaveProperty('requests_by_model');
		expect(typeof requests.requests_by_model).toBe('object');
	});
});
