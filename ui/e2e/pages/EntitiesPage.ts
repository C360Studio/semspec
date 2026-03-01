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
		this.entitiesPage = page.locator('.entities-page');
		this.pageHeader = page.locator('.page-header');
		this.searchInput = page.locator('input[aria-label="Search entities"]');
		this.typeFilter = page.locator('select[aria-label="Filter by type"]');
		this.entityList = page.locator('.entity-list');
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
		await this.typeFilter.selectOption(type);
	}
}
