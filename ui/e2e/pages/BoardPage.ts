import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Board page.
 *
 * Provides methods to interact with and verify:
 * - Plans grid with plan cards
 * - Empty state when no plans exist
 * - Board footer status indicators
 */
export class BoardPage {
	readonly page: Page;
	readonly boardView: Locator;
	readonly boardHeader: Locator;
	readonly plansGrid: Locator;
	readonly emptyState: Locator;
	readonly newPlanBtn: Locator;
	readonly startBtn: Locator;
	readonly boardFooter: Locator;
	readonly loadingState: Locator;
	readonly errorState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.boardView = page.locator('.board-view');
		this.boardHeader = page.locator('.board-header');
		this.plansGrid = page.locator('.plans-grid');
		this.emptyState = this.boardView.locator('.empty-state');
		this.newPlanBtn = page.locator('button.new-plan-btn');
		this.startBtn = page.locator('button.start-btn');
		this.boardFooter = page.locator('.board-footer');
		this.loadingState = this.boardView.locator('.loading-state');
		this.errorState = this.boardView.locator('.error-state');
	}

	async goto(): Promise<void> {
		await this.page.goto('/board');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.boardView).toBeVisible();
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
	}

	async expectNoEmptyState(): Promise<void> {
		await expect(this.emptyState).not.toBeVisible();
	}

	async expectPlansGrid(): Promise<void> {
		await expect(this.plansGrid).toBeVisible();
	}

	async expectPlanCardCount(count: number): Promise<void> {
		const cards = this.plansGrid.locator('.plan-card');
		await expect(cards).toHaveCount(count);
	}

	async clickNewPlan(): Promise<void> {
		await this.newPlanBtn.click();
	}

	async clickStartBtn(): Promise<void> {
		await this.startBtn.click();
	}

	async expectFooterVisible(): Promise<void> {
		await expect(this.boardFooter).toBeVisible();
	}

	async expectHealthyStatus(): Promise<void> {
		const healthyDot = this.boardFooter.locator('.status-dot.healthy');
		await expect(healthyDot).toBeVisible();
	}
}
