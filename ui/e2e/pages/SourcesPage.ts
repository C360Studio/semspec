import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Sources page.
 *
 * NOTE: The /sources route has been removed from the UI.
 * Source management has been offloaded to semsource (external service).
 * Source *ingestion* via the chat interface (URL detection, file upload)
 * is tested via ChatPage instead.
 *
 * This class is kept as a stub for backwards compatibility.
 * Tests that use SourcesPage should be updated to use ChatPage for
 * source suggestion chip interactions.
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
		// /sources route no longer exists — navigating to it will result in a 404 or redirect.
		// Locators are set to non-matching selectors to fail fast if used.
		this.sourcesPage = page.locator('[data-removed="sources-page-removed"]');
		this.pageHeader = page.locator('[data-removed="sources-page-removed"]');
		this.searchInput = page.locator('[data-removed="sources-page-removed"]');
		this.uploadBtn = page.locator('[data-removed="sources-page-removed"]');
		this.addUrlBtn = page.locator('[data-removed="sources-page-removed"]');
		this.addRepoBtn = page.locator('[data-removed="sources-page-removed"]');
		this.sourceList = page.locator('[data-removed="sources-page-removed"]');
		this.emptyState = page.locator('[data-removed="sources-page-removed"]');
		this.loadingState = page.locator('[data-removed="sources-page-removed"]');
	}

	async goto(): Promise<void> {
		// /sources route does not exist — navigate to workspace or entities instead.
		// Tests that called this should be updated or removed.
		await this.page.goto('/workspace');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		// Sources page removed — this assertion will fail intentionally
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
