import { test, expect } from './helpers/setup';

/**
 * Entity Browser E2E Tests
 *
 * These tests run against the real graph-gateway backend via GraphQL.
 * The ast-indexer indexes /workspace and populates the graph with real entities.
 *
 * Only error/loading/empty-state tests use Playwright route mocking,
 * since those UI states require controlled responses that can't be
 * triggered against a live backend.
 */

test.describe('Entity Browser', () => {
	test.describe('Navigation', () => {
		test('sidebar link navigates to /entities', async ({ page, sidebarPage }) => {
			await page.goto('/');
			await sidebarPage.navigateTo('Entities');
			await expect(page).toHaveURL('/entities');
		});

		test('entities page loads with correct title', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectPageTitle('Entity Browser');
		});
	});

	test.describe('Empty State', () => {
		test('shows empty state message when no entities exist', async ({ page, entitiesPage }) => {
			// Must mock: need to guarantee an empty graph
			await page.route('**/graphql', async route => {
				const body = JSON.parse(route.request().postData() || '{}');
				const query = body.query || '';

				if (query.includes('entitiesByPrefix')) {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({ data: { entitiesByPrefix: [] } })
					});
					return;
				}

				if (query.includes('entityIdHierarchy')) {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							data: { entityIdHierarchy: { children: [], totalCount: 0 } }
						})
					});
					return;
				}

				route.continue();
			});

			await entitiesPage.goto();
			await entitiesPage.expectEmptyState();
			await entitiesPage.expectEmptyStateNoData();
		});

		test('shows filtered empty state when search has no results', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);
			await entitiesPage.search('nonexistent-query-xyz-99999');
			await entitiesPage.expectEmptyState();
			await entitiesPage.expectEmptyStateWithFilters();
		});
	});

	test.describe('Entity List', () => {
		test('displays entities from the graph', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);
		});

		test('shows type badges on entity cards', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);
			// Verify at least one entity has a type badge (any type)
			const badges = entitiesPage.page.locator('[data-testid="entity-type-badge"]');
			await expect(badges.first()).toBeVisible();
		});
	});

	test.describe('Type Filter', () => {
		test('filters entities by type', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			const allCount = await entitiesPage.getEntityCount();
			expect(allCount).toBeGreaterThan(0);

			// ast-indexer should produce code entities from /workspace
			await entitiesPage.filterByType('Code');
			const codeCount = await entitiesPage.getEntityCount();
			expect(codeCount).toBeGreaterThan(0);
			expect(codeCount).toBeLessThanOrEqual(allCount);
		});

		test('returns to all entities when filter cleared', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			const allCount = await entitiesPage.getEntityCount();

			await entitiesPage.filterByType('Code');
			await entitiesPage.filterByType('All Types');
			const restoredCount = await entitiesPage.getEntityCount();
			expect(restoredCount).toBe(allCount);
		});
	});

	test.describe('Search', () => {
		test('filters entities by search query', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			const allCount = await entitiesPage.getEntityCount();
			expect(allCount).toBeGreaterThan(0);

			// Search for something that likely won't match all entities
			await entitiesPage.search('nonexistent-query-xyz-99999');
			const filteredCount = await entitiesPage.getEntityCount();
			expect(filteredCount).toBeLessThan(allCount);
		});

		test('clears search returns all entities', async ({ entitiesPage }) => {
			await entitiesPage.goto();
			const allCount = await entitiesPage.getEntityCount();

			await entitiesPage.search('some-filter');
			await entitiesPage.clearSearch();
			const restoredCount = await entitiesPage.getEntityCount();
			expect(restoredCount).toBe(allCount);
		});
	});

	test.describe('Entity Detail Navigation', () => {
		test('clicking entity card navigates to detail page', async ({ page, entitiesPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);

			// Click the first entity card
			const firstCard = entitiesPage.page.locator('[data-testid="entity-card"]').first();
			await firstCard.click();

			await expect(page).toHaveURL(/\/entities\/.+/);
		});
	});

	test.describe('Error Handling', () => {
		test('shows error state on API failure', async ({ page, entitiesPage }) => {
			// Must mock: need to simulate a server error
			await page.route('**/graphql', async route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						errors: [{ message: 'Internal server error' }]
					})
				});
			});
			await entitiesPage.goto();
			await entitiesPage.expectErrorState();
		});
	});

	test.describe('Loading State', () => {
		test('shows loading state while fetching', async ({ page, entitiesPage }) => {
			// Must mock: need controlled delay for timing assertions
			await page.route('**/graphql', async route => {
				const body = JSON.parse(route.request().postData() || '{}');
				const query = body.query || '';

				if (query.includes('entityIdHierarchy')) {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							data: { entityIdHierarchy: { children: [{ name: 'code', count: 1 }], totalCount: 1 } }
						})
					});
					return;
				}

				if (query.includes('entitiesByPrefix')) {
					await new Promise(resolve => setTimeout(resolve, 1000));
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							data: {
								entitiesByPrefix: [{
									id: 'code.file.test',
									triples: [
										{ subject: 'code.file.test', predicate: 'dc.terms.title', object: 'test.go' }
									]
								}]
							}
						})
					});
					return;
				}

				route.continue();
			});

			await page.goto('/entities');
			await entitiesPage.expectLoading();
			await entitiesPage.expectNoLoading();
			await entitiesPage.expectMinimumEntityCount(1);
		});
	});
});

test.describe('Entity Detail Page', () => {
	test.describe('Detail View', () => {
		test('displays entity header information', async ({ page, entitiesPage, entityDetailPage }) => {
			// Navigate to first real entity
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);
			const firstCard = entitiesPage.page.locator('[data-testid="entity-card"]').first();
			await firstCard.click();
			await expect(page).toHaveURL(/\/entities\/.+/);

			// Verify detail page has basic structure
			await entityDetailPage.expectEntityNameVisible();
			await entityDetailPage.expectEntityIdVisible();
		});

		test('displays predicates', async ({ page, entitiesPage, entityDetailPage }) => {
			await entitiesPage.goto();
			await entitiesPage.expectMinimumEntityCount(1);
			const firstCard = entitiesPage.page.locator('[data-testid="entity-card"]').first();
			await firstCard.click();
			await expect(page).toHaveURL(/\/entities\/.+/);

			// Code entities from ast-indexer should have predicates
			await entityDetailPage.expectMinimumPredicateCount(1);
		});
	});

	test.describe('Error Handling', () => {
		test('shows error state when entity not found', async ({ page, entityDetailPage }) => {
			// Must mock: need to simulate a missing entity
			await page.route('**/graphql', async route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						errors: [{ message: 'Entity not found' }]
					})
				});
			});

			await entityDetailPage.goto('nonexistent');
			await entityDetailPage.expectErrorState();
			await entityDetailPage.expectErrorMessage('Entity not found');
		});
	});
});

test.describe('Sidebar Entity Counts', () => {
	test('shows entity count badge in sidebar when entities exist', async ({ page, sidebarPage }) => {
		await page.goto('/');
		// With real graph data from ast-indexer, there should be entities
		await sidebarPage.expectEntityCountVisible();
	});

	test('hides entity count when zero', async ({ page, sidebarPage }) => {
		// Must mock: need to guarantee zero entities
		await page.route('**/graphql', async route => {
			const body = JSON.parse(route.request().postData() || '{}');
			const query = body.query || '';

			if (query.includes('entityIdHierarchy')) {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						data: { entityIdHierarchy: { children: [], totalCount: 0 } }
					})
				});
				return;
			}

			route.continue();
		});

		await page.goto('/');
		await sidebarPage.expectNoEntityCount();
	});
});
