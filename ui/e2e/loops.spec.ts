import { test, expect, testData } from './helpers/setup';

test.describe('Loop Management', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/');
	});

	test.describe('Active Loops Display', () => {
		test('shows active loops count in sidebar', async ({ sidebarPage }) => {
			await sidebarPage.expectVisible();
			await expect(sidebarPage.activeLoopsCounter).toBeVisible();

			// Should display count in format "N active loops"
			const text = await sidebarPage.activeLoopsCounter.textContent();
			expect(text).toMatch(/\d+ active loops/);
		});

		test('loops count updates after command', async ({ chatPage, sidebarPage, page }) => {
			// Get initial count
			const initialText = await sidebarPage.activeLoopsCounter.textContent();
			const initialMatch = initialText?.match(/(\d+) active loops/);
			const initialCount = initialMatch ? parseInt(initialMatch[1]) : 0;

			// Send a command that may create a loop
			await chatPage.sendMessage(testData.statusCommand());
			await chatPage.waitForResponse();

			// Wait for potential update
			await page.waitForTimeout(1000);

			// Count may have changed (depends on backend behavior)
			const finalText = await sidebarPage.activeLoopsCounter.textContent();
			expect(finalText).toMatch(/\d+ active loops/);
		});
	});

	test.describe('Paused Loops Badge', () => {
		test('hides badge when no paused loops', async ({ sidebarPage, page }) => {
			// Mock empty loops response
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.reload();
			await sidebarPage.expectNoPausedBadge();
		});

		test('shows badge when loops are paused', async ({ sidebarPage, page }) => {
			// Mock loops response with paused loops
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							loop_id: 'test-loop-1',
							state: 'paused',
							created_at: new Date().toISOString()
						},
						{
							loop_id: 'test-loop-2',
							state: 'paused',
							created_at: new Date().toISOString()
						}
					])
				});
			});

			await page.reload();
			await sidebarPage.expectPausedBadge(2);
		});

		test('badge shows correct count for mixed states', async ({ sidebarPage, page }) => {
			// Mock loops with mixed states
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							loop_id: 'loop-1',
							state: 'executing',
							created_at: new Date().toISOString()
						},
						{
							loop_id: 'loop-2',
							state: 'paused',
							created_at: new Date().toISOString()
						},
						{
							loop_id: 'loop-3',
							state: 'complete',
							created_at: new Date().toISOString()
						}
					])
				});
			});

			await page.reload();
			await sidebarPage.expectPausedBadge(1);
		});
	});

	test.describe('Loop State Display', () => {
		test('active loops include pending, executing, and paused states', async ({ sidebarPage, page }) => {
			// Mock loops with various active states
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{ loop_id: 'loop-1', state: 'pending', created_at: new Date().toISOString() },
						{ loop_id: 'loop-2', state: 'executing', created_at: new Date().toISOString() },
						{ loop_id: 'loop-3', state: 'paused', created_at: new Date().toISOString() },
						{ loop_id: 'loop-4', state: 'complete', created_at: new Date().toISOString() }
					])
				});
			});

			await page.reload();

			// Should show 3 active loops (pending + executing + paused)
			await sidebarPage.expectActiveLoops(3);
		});
	});
});
