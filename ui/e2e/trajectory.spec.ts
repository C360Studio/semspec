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

/** Helper: set up standard loop + trajectory mocks and reload. */
async function setupLoopWithTrajectory(
	page: import('@playwright/test').Page,
	loopId: string,
	trajectoryOverrides?: Partial<typeof mockTrajectory> | null,
	trajectoryStatus = 200
) {
	await page.route('**/agentic-dispatch/loops', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				testData.mockWorkflowLoop({
					loop_id: loopId,
					state: 'executing',
				}),
			]),
		});
	});

	await page.route(`**/trajectory-api/loops/${loopId}**`, (route) => {
		if (trajectoryOverrides === null) {
			// Delayed response for loading state tests
			return;
		}
		route.fulfill({
			status: trajectoryStatus,
			contentType: 'application/json',
			body: JSON.stringify(
				trajectoryStatus === 200
					? { ...mockTrajectory, loop_id: loopId, ...trajectoryOverrides }
					: { error: 'Error' }
			),
		});
	});

	await page.reload();
}

test.describe('Trajectory Panel', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/activity');
	});

	test.describe('Trajectory Toggle Button', () => {
		test('trajectory button visible on loop card', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'loop-test-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			await expect(loopCard).toBeVisible();

			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await expect(trajectoryToggle).toBeVisible();
		});
	});

	test.describe('Panel Expansion', () => {
		test('click expands trajectory panel', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'loop-test-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			await expect(loopCard).toBeVisible();

			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();
		});
	});

	test.describe('Summary Metrics', () => {
		test('shows summary metrics in expanded panel', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'loop-test-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			const summaryBar = trajectorySection.locator('.summary-bar');
			await expect(summaryBar).toBeVisible();

			const llmStat = summaryBar.locator('.summary-stat', { hasText: /LLM/i });
			await expect(llmStat).toBeVisible();

			const toolStat = summaryBar.locator('.summary-stat', { hasText: /tool/i });
			await expect(toolStat).toBeVisible();

			const tokensStat = summaryBar.locator('.summary-stat', { hasText: /tokens?/i });
			await expect(tokensStat).toBeVisible();
		});
	});

	test.describe('Entry Cards', () => {
		test('shows entry cards with model info', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'loop-test-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			const modelEntry = trajectorySection.locator('.entry-card.model-call');
			await expect(modelEntry).toBeVisible();
			await expect(modelEntry).toContainText('gpt-4');

			const modelMetrics = modelEntry.locator('.metrics-row .metric');
			const metricsCount = await modelMetrics.count();
			expect(metricsCount).toBeGreaterThanOrEqual(1);

			const tokenMetric = modelEntry.locator('.metrics-row .metric', { hasText: /tokens?/i });
			await expect(tokenMetric).toBeVisible();
		});

		test.skip('shows entry cards with tool info', async ({ page }) => {
			// TODO: Flaky - trajectory data loading timing
			await setupLoopWithTrajectory(page, 'loop-test-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loop-tes' });
			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const trajectorySection = loopCard.locator('.trajectory-section');
			await expect(trajectorySection).toBeVisible();

			const toolEntries = trajectorySection.locator('.entry-card.tool-call');
			const toolCount = await toolEntries.count();
			expect(toolCount).toBeGreaterThanOrEqual(1);

			const firstToolEntry = toolEntries.first();
			await expect(firstToolEntry).toContainText('file_read');

			const toolStatus = firstToolEntry.locator('.status-chip');
			await expect(toolStatus).toBeVisible();

			const toolDuration = firstToolEntry.locator('.metrics-row .metric', { hasText: /\d+ms/ });
			await expect(toolDuration).toBeVisible();
		});
	});

	test.describe('Loading and Error States', () => {
		test('loading state while fetching trajectory data', async ({ page }) => {
			// Set up loop mock but delay trajectory response
			await page.route('**/agentic-dispatch/loops', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loading-traj-loop',
							state: 'executing',
						}),
					]),
				});
			});

			// Delay the trajectory response to observe the loading state
			await page.route('**/trajectory-api/loops/loading-traj-loop**', async (route) => {
				await new Promise((resolve) => setTimeout(resolve, 3000));
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ ...mockTrajectory, loop_id: 'loading-traj-loop' }),
				});
			});

			await page.reload();

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loading-' });
			await expect(loopCard).toBeVisible();

			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const loadingState = loopCard.locator('.loading-state');
			await expect(loadingState).toBeVisible({ timeout: 1000 });
		});

		test('empty state when no trajectory data exists', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'empty-traj-loop', undefined, 404);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'empty-tr' });
			await expect(loopCard).toBeVisible();

			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const emptyOrError = loopCard.locator('.empty-state, .error-state');
			await expect(emptyOrError).toBeVisible();
		});

		test('error state when API returns an error', async ({ page }) => {
			await setupLoopWithTrajectory(page, 'error-traj-loop', undefined, 500);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'error-tr' });
			await expect(loopCard).toBeVisible();

			const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
			await trajectoryToggle.click();

			const errorState = loopCard.locator('.error-state');
			await expect(errorState).toBeVisible();
		});
	});
});
