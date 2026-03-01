import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Sources page.
 *
 * Provides methods to interact with and verify:
 * - Page header and action buttons
 * - Search input and type filters
 * - Source list
 */
export class SourcesPage {
	readonly page: Page;
	readonly sourcesPage: Locator;
	readonly pageHeader: Locator;
	readonly searchInput: Locator;
	readonly uploadBtn: Locator;
	readonly addUrlBtn: Locator;
	readonly addRepoBtn: Locator;
	readonly sourceList: Locator;
	readonly emptyState: Locator;
	readonly loadingState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.sourcesPage = page.locator('.sources-page');
		this.pageHeader = page.locator('.page-header');
		this.searchInput = page.locator('input[aria-label="Search sources"]');
		this.uploadBtn = page.locator('.header-actions button.action-button.primary');
		this.addUrlBtn = page.locator('.header-actions button.action-button.secondary').first();
		this.addRepoBtn = page.locator('.header-actions button.action-button.secondary').nth(1);
		this.sourceList = page.locator('.source-list');
		this.emptyState = this.sourcesPage.locator('.empty-state');
		this.loadingState = this.sourcesPage.locator('.loading-state');
	}

	async goto(): Promise<void> {
		await this.page.goto('/sources');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.sourcesPage).toBeVisible();
	}

	async expectHeaderText(text: string): Promise<void> {
		await expect(this.pageHeader).toContainText(text);
	}

	async expectSearchVisible(): Promise<void> {
		await expect(this.searchInput).toBeVisible();
	}

	async expectUploadBtnVisible(): Promise<void> {
		await expect(this.uploadBtn).toBeVisible();
	}

	async search(text: string): Promise<void> {
		await this.searchInput.fill(text);
	}
}
