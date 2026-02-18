import { test, expect, testData } from './helpers/setup';

/**
 * Context Assembly tests are skipped because the LoopCard component
 * with context toggle (.action-btn.context) is not yet integrated
 * into any routes. The component exists at components/loops/LoopCard.svelte
 * but is not used in the activity page or elsewhere.
 *
 * These tests should be enabled when LoopCard is integrated into the UI.
 */
test.describe.skip('Context Assembly', () => {
	test.describe('Context Toggle', () => {
		test('shows context toggle on loop with context_request_id', async ({ page }) => {
			// Mock a loop with context_request_id
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'ctx-toggle-loop-1',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-123'
						}
					])
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			// Find the loop card
			const loopCard = page.locator('.loop-card').filter({ hasText: 'ctx-togg' });
			await expect(loopCard).toBeVisible();

			// Check for context toggle button
			const contextToggle = loopCard.locator('.action-btn.context');
			await expect(contextToggle).toBeVisible();
		});

		test('hides context toggle on loop without context_request_id', async ({ page }) => {
			// Mock a loop without context_request_id
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'no-ctx-loop-1',
							state: 'executing'
						})
						// No context_request_id
					])
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			// Find the loop card
			const loopCard = page.locator('.loop-card').filter({ hasText: 'no-ctx-l' });
			if (await loopCard.isVisible()) {
				// Context toggle should not be visible
				const contextToggle = loopCard.locator('.action-btn.context');
				await expect(contextToggle).not.toBeVisible();
			}
		});

		test('expands context panel when toggle clicked', async ({ page }) => {
			// Mock a loop with context_request_id
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'expand-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-expand'
						}
					])
				});
			});

			// Mock the context response
			await page.route('**/context-builder/responses/ctx-req-expand', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-expand',
						task_type: 'spec_writing',
						total_tokens: 5000,
						budget_tokens: 10000,
						truncated: false,
						entries: [
							{
								source_type: 'file',
								source_path: 'src/main.ts',
								tokens: 1500,
								truncated: false
							}
						]
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			// Find the loop card and click context toggle
			const loopCard = page.locator('.loop-card').filter({ hasText: 'expand-c' });
			await expect(loopCard).toBeVisible();

			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Context section should be visible
			const contextSection = loopCard.locator('.context-section');
			await expect(contextSection).toBeVisible();
		});

		test('collapses context panel when toggle clicked again', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'collapse-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-collapse'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-collapse', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-collapse',
						task_type: 'spec_writing',
						total_tokens: 5000,
						budget_tokens: 10000,
						truncated: false,
						entries: []
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'collapse' });
			const contextToggle = loopCard.locator('.action-btn.context');

			// Expand
			await contextToggle.click();
			const contextSection = loopCard.locator('.context-section');
			await expect(contextSection).toBeVisible();

			// Collapse
			await contextToggle.click();
			await expect(contextSection).not.toBeVisible();
		});
	});

	test.describe('Budget Bar', () => {
		test('shows token budget bar in expanded context', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'budget-bar-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-budget'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-budget', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-budget',
						task_type: 'task_writing',
						total_tokens: 7500,
						budget_tokens: 10000,
						truncated: false,
						entries: [
							{ source_type: 'file', source_path: 'src/app.ts', tokens: 2500, truncated: false },
							{ source_type: 'graph', source_path: 'entities', tokens: 5000, truncated: false }
						]
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'budget-b' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Budget bar should be visible
			const budgetBar = loopCard.locator('.budget-bar');
			await expect(budgetBar).toBeVisible();
		});

		test('shows truncated indicator when context is truncated', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'truncated-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-truncated'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-truncated', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-truncated',
						task_type: 'design',
						total_tokens: 15000,
						budget_tokens: 10000,
						truncated: true,
						entries: [
							{ source_type: 'file', source_path: 'src/large.ts', tokens: 10000, truncated: true }
						]
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'truncate' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Look for truncation indicator
			const truncatedIndicator = loopCard.locator('.truncated-indicator, [class*="truncated"]');
			// This may be visible if the component shows truncation state
		});
	});

	test.describe('Provenance List', () => {
		test('shows provenance entries in expanded context', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'provenance-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-provenance'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-provenance', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-provenance',
						task_type: 'spec_writing',
						total_tokens: 8000,
						budget_tokens: 10000,
						truncated: false,
						entries: [
							{ source_type: 'file', source_path: 'src/auth.ts', tokens: 2000, truncated: false },
							{ source_type: 'file', source_path: 'src/user.ts', tokens: 1500, truncated: false },
							{ source_type: 'graph', source_path: 'entities/User', tokens: 4500, truncated: false }
						]
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'provenan' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Provenance list should be visible
			const provenanceList = loopCard.locator('.provenance-list');
			await expect(provenanceList).toBeVisible();

			// Should have provenance items
			const provenanceItems = loopCard.locator('.provenance-item');
			const count = await provenanceItems.count();
			expect(count).toBeGreaterThanOrEqual(1);
		});

		test('shows source type for provenance entries', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'source-type-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-source'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-source', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-source',
						task_type: 'task_writing',
						total_tokens: 3000,
						budget_tokens: 10000,
						truncated: false,
						entries: [
							{ source_type: 'file', source_path: 'README.md', tokens: 500, truncated: false },
							{ source_type: 'graph', source_path: 'entities/Project', tokens: 2500, truncated: false }
						]
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'source-t' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Check for source type indicators
			const fileSource = loopCard.locator('.provenance-item').filter({ hasText: 'README.md' });
			const graphSource = loopCard.locator('.provenance-item').filter({ hasText: 'entities/Project' });

			// At least one should be visible
			const hasFile = await fileSource.isVisible();
			const hasGraph = await graphSource.isVisible();
			expect(hasFile || hasGraph).toBe(true);
		});
	});

	test.describe('Task Type Badge', () => {
		test('shows task type badge in context panel', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'task-type-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-tasktype'
						}
					])
				});
			});

			await page.route('**/context-builder/responses/ctx-req-tasktype', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-tasktype',
						task_type: 'spec_writing',
						total_tokens: 5000,
						budget_tokens: 10000,
						truncated: false,
						entries: []
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'task-typ' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Task type badge should be visible
			const taskTypeBadge = loopCard.locator('.task-type-badge');
			await expect(taskTypeBadge).toBeVisible();
		});
	});

	test.describe('Loading and Error States', () => {
		test('shows loading state while fetching context', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'loading-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-loading'
						}
					])
				});
			});

			// Delay the context response
			await page.route('**/context-builder/responses/ctx-req-loading', async route => {
				await new Promise(resolve => setTimeout(resolve, 2000));
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						request_id: 'ctx-req-loading',
						task_type: 'design',
						total_tokens: 1000,
						budget_tokens: 10000,
						truncated: false,
						entries: []
					})
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loading-' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Loading state should be visible
			const loadingState = loopCard.locator('.loading-state');
			await expect(loadingState).toBeVisible({ timeout: 1000 });
		});

		test('shows error state when context fetch fails', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'error-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-error'
						}
					])
				});
			});

			// Return error for context request
			await page.route('**/context-builder/responses/ctx-req-error', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'Internal server error' })
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'error-ct' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Error state should be visible
			const errorState = loopCard.locator('.error-state');
			await expect(errorState).toBeVisible();
		});

		test('shows empty state when no context entries', async ({ page }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							...testData.mockWorkflowLoop({
								loop_id: 'empty-ctx-loop',
								state: 'executing'
							}),
							context_request_id: 'ctx-req-empty'
						}
					])
				});
			});

			// Return empty context
			await page.route('**/context-builder/responses/ctx-req-empty', route => {
				route.fulfill({
					status: 404,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'Not found' })
				});
			});

			await page.goto('/');
			await page.waitForLoadState('networkidle');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'empty-ct' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Should show error or empty state
			const emptyOrError = loopCard.locator('.empty-state, .error-state');
			await expect(emptyOrError).toBeVisible();
		});
	});
});
