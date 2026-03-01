import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Plans list page.
 *
 * Provides methods to interact with and verify:
 * - Plan rows with slug and stage badges
 * - Stage filter and sort controls
 * - Empty state
 */
export class PlansListPage {
	readonly page: Page;
	readonly plansView: Locator;
	readonly planRows: Locator;
	readonly stageFilter: Locator;
	readonly sortBy: Locator;
	readonly newPlanLink: Locator;
	readonly emptyState: Locator;
	readonly loadingState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.plansView = page.locator('.plans-view');
		this.planRows = page.locator('.plan-row');
		this.stageFilter = page.locator('select#stage-filter');
		this.sortBy = page.locator('select#sort-by');
		this.newPlanLink = page.locator('a.new-plan-btn');
		this.emptyState = this.plansView.locator('.empty-state');
		this.loadingState = this.plansView.locator('.loading-state');
	}

	async goto(): Promise<void> {
		await this.page.goto('/plans');
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.plansView).toBeVisible();
	}

	async expectPlanCount(count: number): Promise<void> {
		await expect(this.planRows).toHaveCount(count);
	}

	async expectPlanRowWithSlug(slug: string): Promise<void> {
		const row = this.planRows.filter({ hasText: slug });
		await expect(row).toBeVisible();
	}

	async expectPlanStage(slug: string, stage: string): Promise<void> {
		const row = this.planRows.filter({ hasText: slug });
		const stageBadge = row.locator('.plan-stage');
		await expect(stageBadge).toHaveAttribute('data-stage', stage);
	}

	async filterByStage(stage: string): Promise<void> {
		await this.stageFilter.selectOption(stage);
	}

	async sortByField(field: string): Promise<void> {
		await this.sortBy.selectOption(field);
	}

	async clickPlanRow(slug: string): Promise<void> {
		const row = this.planRows.filter({ hasText: slug });
		await row.click();
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
	}
}
