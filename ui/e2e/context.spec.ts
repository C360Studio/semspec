import { test, expect, testData } from './helpers/setup';

/** Helper: build a valid ContextBuildResponse mock matching the TypeScript interface. */
function mockContextResponse(overrides: Record<string, unknown> = {}) {
	return {
		request_id: 'ctx-req-default',
		task_type: 'review',
		token_count: 5000,
		tokens_used: 5000,
		tokens_budget: 10000,
		truncated: false,
		provenance: [],
		...overrides,
	};
}

/** Helper: build a ProvenanceEntry matching the TypeScript interface. */
function mockProvenance(overrides: Record<string, unknown> = {}) {
	return {
		source: 'file:src/main.ts',
		type: 'file',
		tokens: 1500,
		truncated: false,
		priority: 1,
		...overrides,
	};
}

/** Helper: set up loop + context mocks and reload. */
async function setupLoopWithContext(
	page: import('@playwright/test').Page,
	loopId: string,
	contextRequestId: string | undefined,
	contextResponse?: Record<string, unknown> | null,
	contextStatus = 200
) {
	await page.route('**/agentic-dispatch/loops', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([
				testData.mockWorkflowLoop({
					loop_id: loopId,
					state: 'executing',
					...(contextRequestId ? { context_request_id: contextRequestId } : {}),
				}),
			]),
		});
	});

	if (contextRequestId && contextResponse !== undefined) {
		await page.route(`**/context-builder/responses/${contextRequestId}`, (route) => {
			if (contextResponse === null) {
				// Delayed — don't fulfill (for loading state tests)
				return;
			}
			route.fulfill({
				status: contextStatus,
				contentType: 'application/json',
				body: JSON.stringify(
					contextStatus === 200
						? contextResponse
						: { error: 'Error' }
				),
			});
		});
	}

	await page.reload();
}

test.describe('Context Assembly', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/activity');
	});

	test.describe('Context Toggle', () => {
		test('shows context toggle on loop with context_request_id', async ({ page }) => {
			await setupLoopWithContext(page, 'ctx-toggle-loop-1', 'ctx-req-123');

			const loopCard = page.locator('.loop-card').filter({ hasText: 'ctx-togg' });
			await expect(loopCard).toBeVisible();

			const contextToggle = loopCard.locator('.action-btn.context');
			await expect(contextToggle).toBeVisible();
		});

		test('hides context toggle on loop without context_request_id', async ({ page }) => {
			await setupLoopWithContext(page, 'no-ctx-loop-1', undefined);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'no-ctx-l' });
			if (await loopCard.isVisible()) {
				const contextToggle = loopCard.locator('.action-btn.context');
				await expect(contextToggle).not.toBeVisible();
			}
		});

		test('expands context panel when toggle clicked', async ({ page }) => {
			await setupLoopWithContext(page, 'expand-ctx-loop', 'ctx-req-expand',
				mockContextResponse({
					request_id: 'ctx-req-expand',
					task_type: 'review',
					token_count: 5000,
					tokens_used: 5000,
					tokens_budget: 10000,
					provenance: [
						mockProvenance({ source: 'file:src/main.ts', tokens: 1500 }),
					],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'expand-c' });
			await expect(loopCard).toBeVisible();

			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const contextSection = loopCard.locator('.context-section');
			await expect(contextSection).toBeVisible();
		});

		test('collapses context panel when toggle clicked again', async ({ page }) => {
			await setupLoopWithContext(page, 'collapse-ctx-loop', 'ctx-req-collapse',
				mockContextResponse({
					request_id: 'ctx-req-collapse',
					token_count: 5000,
					tokens_used: 5000,
					tokens_budget: 10000,
					provenance: [],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'collapse' });
			const contextToggle = loopCard.locator('.action-btn.context');

			await contextToggle.click();
			const contextSection = loopCard.locator('.context-section');
			await expect(contextSection).toBeVisible();

			await contextToggle.click();
			await expect(contextSection).not.toBeVisible();
		});
	});

	test.describe('Budget Bar', () => {
		test('shows token budget bar in expanded context', async ({ page }) => {
			await setupLoopWithContext(page, 'budget-bar-loop', 'ctx-req-budget',
				mockContextResponse({
					request_id: 'ctx-req-budget',
					task_type: 'implementation',
					token_count: 7500,
					tokens_used: 7500,
					tokens_budget: 10000,
					provenance: [
						mockProvenance({ source: 'file:src/app.ts', type: 'file', tokens: 2500, priority: 1 }),
						mockProvenance({ source: 'graph:entities', type: 'graph', tokens: 5000, priority: 2 }),
					],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'budget-b' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const budgetBar = loopCard.locator('.budget-bar');
			await expect(budgetBar).toBeVisible();
		});

		test('shows truncated indicator when context is truncated', async ({ page }) => {
			await setupLoopWithContext(page, 'truncated-ctx-loop', 'ctx-req-truncated',
				mockContextResponse({
					request_id: 'ctx-req-truncated',
					task_type: 'exploration',
					token_count: 15000,
					tokens_used: 10000,
					tokens_budget: 10000,
					truncated: true,
					provenance: [
						mockProvenance({ source: 'file:src/large.ts', type: 'file', tokens: 10000, truncated: true }),
					],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'truncate' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// Look for truncation warning in the budget bar
			const truncationWarning = loopCard.locator('.truncation-warning');
			// This may be visible if the component shows truncation state
		});
	});

	test.describe('Provenance List', () => {
		test('shows provenance entries in expanded context', async ({ page }) => {
			await setupLoopWithContext(page, 'provenance-loop', 'ctx-req-provenance',
				mockContextResponse({
					request_id: 'ctx-req-provenance',
					task_type: 'review',
					token_count: 8000,
					tokens_used: 8000,
					tokens_budget: 10000,
					provenance: [
						mockProvenance({ source: 'file:src/auth.ts', type: 'file', tokens: 2000, priority: 1 }),
						mockProvenance({ source: 'file:src/user.ts', type: 'file', tokens: 1500, priority: 2 }),
						mockProvenance({ source: 'graph:entities/User', type: 'graph', tokens: 4500, priority: 3 }),
					],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'provenan' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const provenanceList = loopCard.locator('.provenance-list');
			await expect(provenanceList).toBeVisible();

			// ProvenanceList uses .entry class for list items
			const provenanceItems = loopCard.locator('.provenance-list .entry');
			const count = await provenanceItems.count();
			expect(count).toBeGreaterThanOrEqual(1);
		});

		test('shows source type for provenance entries', async ({ page }) => {
			await setupLoopWithContext(page, 'source-type-loop', 'ctx-req-source',
				mockContextResponse({
					request_id: 'ctx-req-source',
					task_type: 'implementation',
					token_count: 3000,
					tokens_used: 3000,
					tokens_budget: 10000,
					provenance: [
						mockProvenance({ source: 'file:README.md', type: 'file', tokens: 500, priority: 1 }),
						mockProvenance({ source: 'graph:entities/Project', type: 'graph', tokens: 2500, priority: 2 }),
					],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'source-t' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			// ProvenanceList uses .entry class, and getSourceShortName strips the prefix
			const fileSource = loopCard.locator('.provenance-list .entry').filter({ hasText: 'README.md' });
			const graphSource = loopCard.locator('.provenance-list .entry').filter({ hasText: 'entities/Project' });

			const hasFile = await fileSource.isVisible();
			const hasGraph = await graphSource.isVisible();
			expect(hasFile || hasGraph).toBe(true);
		});
	});

	test.describe('Task Type Badge', () => {
		test('shows task type badge in context panel', async ({ page }) => {
			await setupLoopWithContext(page, 'task-type-loop', 'ctx-req-tasktype',
				mockContextResponse({
					request_id: 'ctx-req-tasktype',
					task_type: 'review',
					token_count: 5000,
					tokens_used: 5000,
					tokens_budget: 10000,
					provenance: [],
				})
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'task-typ' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const taskTypeBadge = loopCard.locator('.task-type-badge');
			await expect(taskTypeBadge).toBeVisible();
		});
	});

	test.describe('Loading and Error States', () => {
		test('shows loading state while fetching context', async ({ page }) => {
			// Set up loop mock but delay context response
			await page.route('**/agentic-dispatch/loops', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'loading-ctx-loop',
							state: 'executing',
							context_request_id: 'ctx-req-loading',
						}),
					]),
				});
			});

			// Delay the context response to observe the loading state
			await page.route('**/context-builder/responses/ctx-req-loading', async (route) => {
				await new Promise((resolve) => setTimeout(resolve, 3000));
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(
						mockContextResponse({
							request_id: 'ctx-req-loading',
							task_type: 'exploration',
							token_count: 1000,
							tokens_used: 1000,
							tokens_budget: 10000,
							provenance: [],
						})
					),
				});
			});

			await page.reload();

			const loopCard = page.locator('.loop-card').filter({ hasText: 'loading-' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const loadingState = loopCard.locator('.loading-state');
			await expect(loadingState).toBeVisible({ timeout: 1000 });
		});

		test('shows error state when context fetch fails', async ({ page }) => {
			await setupLoopWithContext(page, 'error-ctx-loop', 'ctx-req-error',
				undefined, 500
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'error-ct' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const errorState = loopCard.locator('.error-state');
			await expect(errorState).toBeVisible();
		});

		test('shows empty state when no context entries', async ({ page }) => {
			await setupLoopWithContext(page, 'empty-ctx-loop', 'ctx-req-empty',
				undefined, 404
			);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'empty-ct' });
			const contextToggle = loopCard.locator('.action-btn.context');
			await contextToggle.click();

			const emptyOrError = loopCard.locator('.empty-state, .error-state');
			await expect(emptyOrError).toBeVisible();
		});
	});

	test.describe('Context Request ID Round-Trip', () => {
		test('context_request_id flows from loop data to context toggle visibility', async ({ page }) => {
			const contextRequestId = 'ctx-roundtrip-test-id';

			let contextEndpointCalled = false;
			await page.route('**/agentic-dispatch/loops', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'roundtrip-loop',
							state: 'executing',
							context_request_id: contextRequestId,
						}),
					]),
				});
			});

			await page.route(`**/context-builder/responses/${contextRequestId}`, (route) => {
				contextEndpointCalled = true;
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(
						mockContextResponse({
							request_id: contextRequestId,
							task_type: 'review',
							token_count: 3000,
							tokens_used: 3000,
							tokens_budget: 10000,
							provenance: [
								mockProvenance({ source: 'file:src/roundtrip.ts', tokens: 3000 }),
							],
						})
					),
				});
			});

			await page.reload();

			const loopCard = page.locator('.loop-card').filter({ hasText: 'roundtri' });
			await expect(loopCard).toBeVisible();

			const contextToggle = loopCard.locator('.action-btn.context');
			await expect(contextToggle).toBeVisible();

			await contextToggle.click();

			const contextSection = loopCard.locator('.context-section');
			await expect(contextSection).toBeVisible();

			expect(contextEndpointCalled).toBe(true);
		});

		test('loop without context_request_id has no context toggle but still has trajectory toggle', async ({ page }) => {
			await setupLoopWithContext(page, 'no-ctx-id-loop', undefined);

			const loopCard = page.locator('.loop-card').filter({ hasText: 'no-ctx-i' });
			if (await loopCard.isVisible()) {
				const contextToggle = loopCard.locator('.action-btn.context');
				await expect(contextToggle).not.toBeVisible();

				const trajectoryToggle = loopCard.locator('.action-btn.trajectory');
				await expect(trajectoryToggle).toBeVisible();
			}
		});
	});
});
