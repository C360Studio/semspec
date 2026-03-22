import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Entities page.
 *
 * Provides methods to interact with and verify:
 * - Page header
 * - Search input and type filter
 * - Entity list
 */
export class EntitiesPage {
	readonly page: Page;
	readonly entitiesPage: Locator;
	readonly pageHeader: Locator;
	readonly searchInput: Locator;
	readonly typeFilter: Locator;
	readonly entityList: Locator;
	readonly emptyState: Locator;
	readonly loadingState: Locator;

	constructor(page: Page) {
		this.page = page;
		// Entities page is now a Knowledge Graph Explorer using ThreePanelLayout
		// The center panel has data-testid="graph-page"
		this.entitiesPage = page.locator('[data-testid="graph-page"]');
		// GraphFilters toolbar with data-testid="graph-filters" serves as the header
		this.pageHeader = page.locator('[data-testid="graph-filters"]');
		// Search input in GraphFilters: aria-label="Filter entities or search with natural language"
		this.searchInput = page.locator('[data-testid="graph-search-input"]');
		// Type filter is the .type-filter-chips in GraphFilters (not a <select>)
		this.typeFilter = page.locator('.type-filter-chips');
		// No .entity-list — entities are displayed as graph nodes in SigmaCanvas
		// Use the canvas element as the entity "list"
		this.entityList = page.locator('.graph-canvas, canvas').first();
		this.emptyState = this.entitiesPage.locator('.empty-state');
		this.loadingState = this.entitiesPage.locator('.loading-state');
	}

	async goto(): Promise<void> {
		await this.page.goto('/entities');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.entitiesPage).toBeVisible();
	}

	async expectHeaderText(text: string): Promise<void> {
		await expect(this.pageHeader).toContainText(text);
	}

	async expectSearchVisible(): Promise<void> {
		await expect(this.searchInput).toBeVisible();
	}

	async expectTypeFilterVisible(): Promise<void> {
		await expect(this.typeFilter).toBeVisible();
	}

	async filterByType(type: string): Promise<void> {
		// GraphFilters uses button chips for type filtering (not a <select>)
		// Click the chip with the matching type label
		const chip = this.typeFilter.locator('button').filter({ hasText: type });
		await chip.click();
	}
}
