import { test, expect, testData } from './helpers/setup';

const mockTrajectory = {
	loop_id: 'loop-test-123',
	trace_id: 'trace-test-456',
	steps: 3,
	tool_calls: 2,
	model_calls: 1,
	tokens_in: 1500,
	tokens_out: 500,
	duration_ms: 2500,
	status: 'running',
	entries: [
		{
			type: 'model_call',
			timestamp: new Date().toISOString(),
			duration_ms: 1200,
			model: 'gpt-4',
			provider: 'openai',
			capability: 'planning',
			tokens_in: 1500,
			tokens_out: 500,
			finish_reason: 'stop',
			messages_count: 3,
			response_preview: 'Let me analyze the code...',
		},
		{
			type: 'tool_call',
			timestamp: new Date(Date.now() + 1000).toISOString(),
			duration_ms: 50,
			tool_name: 'file_read',
			status: 'success',
			result_preview: 'package main...',
		},
		{
			type: 'tool_call',
			timestamp: new Date(Date.now() + 2000).toISOString(),
			duration_ms: 30,
			tool_name: 'git_status',
			status: 'success',
			result_preview: 'On branch main...',
		},
	],
};

test.describe.skip('Trajectory Panel', () => {
	test.describe('Trajectory Toggle Button', () => {
		test('trajectory button visible on loop card', async ({ page }) => {
			// Mock the loops endpoint with a loop that has a known loop_id
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-test-123',
							state: 'executing'
						})
					])
				});
			});

			// Mock the trajectory endpoint
			await page.route('**/trajectory-api/loops/loop-test-123', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockTrajectory)
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			// Find the loop card
			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			await expect(loopCard).toBeVisible();

			// The trajectory toggle button should be present on the loop card
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await expect(trajectoryToggle).toBeVisible();
		});
	});

	test.describe('Panel Expansion', () => {
		test('click expands trajectory panel', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-test-123',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/trajectory-api/loops/loop-test-123', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockTrajectory)
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			await expect(loopCard).toBeVisible();

			// Click the trajectory toggle button
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			// The trajectory panel/section should now be visible
			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();
		});
	});

	test.describe('Summary Metrics', () => {
		test('shows summary metrics in expanded panel', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-test-123',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/trajectory-api/loops/loop-test-123', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockTrajectory)
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			// Model calls count should be displayed
			const modelCallsMetric = trajectorySection.locator('.metric', { hasText: /model.calls?/i });
			await expect(modelCallsMetric).toBeVisible();

			// Tool calls count should be displayed
			const toolCallsMetric = trajectorySection.locator('.metric', { hasText: /tool.calls?/i });
			await expect(toolCallsMetric).toBeVisible();

			// Total tokens should be displayed (tokens_in + tokens_out)
			const tokensMetric = trajectorySection.locator('.metric', { hasText: /tokens?/i });
			await expect(tokensMetric).toBeVisible();

			// Duration should be displayed
			const durationMetric = trajectorySection.locator('.metric', { hasText: /duration|ms/i });
			await expect(durationMetric).toBeVisible();
		});
	});

	test.describe('Entry Cards', () => {
		test('shows entry cards with model info', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-test-123',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/trajectory-api/loops/loop-test-123', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockTrajectory)
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			// Model call entry should show the model name
			const modelEntry = trajectorySection.locator('.trajectory-entry.model-call');
			await expect(modelEntry).toBeVisible();
			await expect(modelEntry).toContainText('gpt-4');

			// Model entry should display token information
			const modelTokens = modelEntry.locator('[class*="token"]');
			await expect(modelTokens).toBeVisible();

			// Model entry should offer a way to view the response preview
			const responsePreview = modelEntry.locator('[class*="preview"], [class*="response"]');
			await expect(responsePreview).toBeVisible();
		});

		test('shows entry cards with tool info', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-test-123',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/trajectory-api/loops/loop-test-123', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockTrajectory)
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			// Tool call entries should be present
			const toolEntries = trajectorySection.locator('.trajectory-entry.tool-call');
			const toolCount = await toolEntries.count();
			expect(toolCount).toBeGreaterThanOrEqual(1);

			// First tool entry should show the tool name
			const firstToolEntry = toolEntries.first();
			await expect(firstToolEntry).toContainText('file_read');

			// Tool entry should show status
			const toolStatus = firstToolEntry.locator('[class*="status"]');
			await expect(toolStatus).toBeVisible();

			// Tool entry should show duration
			const toolDuration = firstToolEntry.locator('[class*="duration"], [class*="ms"]');
			await expect(toolDuration).toBeVisible();
		});
	});

	test.describe('Loading and Error States', () => {
		test('loading state while fetching trajectory data', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loading-traj-loop',
							state: 'executing'
						})
					])
				});
			});

			// Delay the trajectory response to observe the loading state
			await page.route('**/trajectory-api/loops/loading-traj-loop', async route => {
				await new Promise(resolve => setTimeout(resolve, 2000));
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ ...mockTrajectory, loop_id: 'loading-traj-loop' })
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loading-' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			// While the request is in flight, a loading indicator should appear
			const loadingState = loopCard.locator('.loading-state');
			await expect(loadingState).toBeVisible({ timeout: 1000 });
		});

		test('empty state when no trajectory data exists', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'empty-traj-loop',
							state: 'executing'
						})
					])
				});
			});

			// Return 404 to represent no trajectory data yet
			await page.route('**/trajectory-api/loops/empty-traj-loop', route => {
				route.fulfill({
					status: 404,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'No trajectory data found' })
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'empty-tr' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			// Should show an empty or not-found state message
			const emptyState = loopCard.locator('.empty-state, .trajectory-section [class*="empty"]');
			await expect(emptyState).toBeVisible();
		});

		test('error state when API returns an error', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'error-traj-loop',
							state: 'executing'
						})
					])
				});
			});

			// Return 500 to simulate a server error
			await page.route('**/trajectory-api/loops/error-traj-loop', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'Internal server error' })
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'error-tr' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			// Error state should be rendered inside the trajectory section
			const errorState = loopCard.locator('.error-state, .trajectory-section [class*="error"]');
			await expect(errorState).toBeVisible();
		});
	});
});
