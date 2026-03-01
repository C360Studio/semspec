import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Settings page.
 *
 * Provides methods to interact with and verify:
 * - Appearance section (theme, reduced motion)
 * - Data & Storage section (activity limit, clear buttons)
 * - About section (version, API)
 */
export class SettingsPage {
	readonly page: Page;
	readonly settingsPage: Locator;
	readonly pageHeader: Locator;
	readonly sections: Locator;
	readonly themeSelect: Locator;
	readonly reducedMotion: Locator;
	readonly activityLimit: Locator;
	readonly aboutRows: Locator;

	constructor(page: Page) {
		this.page = page;
		this.settingsPage = page.locator('.settings-page');
		this.pageHeader = page.locator('.page-header');
		this.sections = page.locator('.settings-section');
		this.themeSelect = page.locator('select#theme-select');
		this.reducedMotion = page.locator('input#reduced-motion');
		this.activityLimit = page.locator('select#activity-limit');
		this.aboutRows = page.locator('.about-row');
	}

	async goto(): Promise<void> {
		await this.page.goto('/settings');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.settingsPage).toBeVisible();
	}

	async expectSections(count: number): Promise<void> {
		await expect(this.sections).toHaveCount(count);
	}

	async expectSectionTitles(titles: string[]): Promise<void> {
		for (const title of titles) {
			const section = this.sections.filter({ hasText: title });
			await expect(section).toBeVisible();
		}
	}

	async expectThemeValue(value: string): Promise<void> {
		await expect(this.themeSelect).toHaveValue(value);
	}

	async selectTheme(value: string): Promise<void> {
		await this.themeSelect.selectOption(value);
	}

	async expectAboutVisible(): Promise<void> {
		await expect(this.aboutRows.first()).toBeVisible();
	}
}
