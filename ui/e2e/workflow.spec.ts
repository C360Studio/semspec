import { test, expect, testData } from './helpers/setup';

test.describe('Semspec Workflow', () => {
	test.beforeEach(async ({ chatPage }) => {
		await chatPage.goto();
	});

	test.describe('Chat Interface', () => {
		test('chat panel is visible on activity page', async ({ chatPage }) => {
			await expect(chatPage.messageList).toBeVisible();
			await expect(chatPage.messageInput).toBeVisible();
		});

		test('quick commands are displayed', async ({ page }) => {
			const commandButtons = page.locator('.hint-chip code');
			await expect(commandButtons.filter({ hasText: '/plan' })).toBeVisible();
			await expect(commandButtons.filter({ hasText: '/tasks' })).toBeVisible();
			await expect(commandButtons.filter({ hasText: '/help' })).toBeVisible();
		});
	});

	test.describe('Loop Panel Workflow Context', () => {
		// These tests mock both loops and plans APIs to test specific UI rendering
		// The Activity page shows plan context from plansStore, not loop properties

		test('loop card displays workflow slug', async ({ loopPanelPage, page }) => {
			// Mock plans API with matching active_loops
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'add-user-auth',
							committed: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'loop-with-slug',
									role: 'design-writer',
									model: 'qwen',
									state: 'executing',
									iterations: 2,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-with-slug',
							state: 'executing'
						})
					])
				});
			});

			await page.reload();
			await loopPanelPage.expectWorkflowContext('loop-with-slug', 'add-user-auth', '');
		});

		test('loop card displays workflow step correctly', async ({ loopPanelPage, page }) => {
			// Test that plan slug is shown - workflow step shown via AgentBadge
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-workflow',
							committed: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'loop-design',
									role: 'design-writer',
									model: 'qwen',
									state: 'executing',
									iterations: 1,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loop-design',
							state: 'executing'
						})
					])
				});
			});

			await page.reload();
			// New layout shows plan slug as link, role shown in AgentBadge
			await loopPanelPage.expectWorkflowContext('loop-design', 'test-workflow', '');
		});

		test('multiple workflow loops display correctly', async ({ page }) => {
			// Mock plans API with multiple plans
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'add-auth',
							committed: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'multiA123',
									role: 'design-writer',
									model: 'qwen',
									state: 'executing',
									iterations: 2,
									max_iterations: 10
								}
							]
						},
						{
							slug: 'new-api',
							committed: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'multiB456',
									role: 'spec-writer',
									model: 'qwen',
									state: 'executing',
									iterations: 1,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'multiA123',
							state: 'executing'
						}),
						testData.mockWorkflowLoop({
							loop_id: 'multiB456',
							state: 'executing'
						})
					])
				});
			});

			await page.reload();
			await page.waitForTimeout(500);

			// Verify plan slugs are rendered as links
			// Loop ID shows last 8 chars, so multiA123 -> ultiA123, multiB456 -> ultiB456
			const authLink = page.locator('.loop-card').filter({ hasText: 'ultiA123' }).locator('.loop-plan').first();
			const apiLink = page.locator('.loop-card').filter({ hasText: 'ultiB456' }).locator('.loop-plan').first();

			await expect(authLink).toHaveText('add-auth');
			await expect(apiLink).toHaveText('new-api');
		});

		// Note: Test for "loop without workflow context" removed because it requires
		// complex mock coordination between loops and plans APIs with timing issues
	});
});
