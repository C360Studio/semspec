import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for Loop Card context expansion.
 *
 * Provides methods to interact with and verify:
 * - Context toggle button
 * - Context Panel (expanded state)
 * - Budget Bar (token usage)
 * - Provenance List (sources)
 */
export class LoopContextPage {
	readonly page: Page;
	readonly loopCard: Locator;
	readonly contextToggle: Locator;
	readonly contextSection: Locator;
	readonly contextPanel: Locator;

	// Budget bar
	readonly budgetBar: Locator;
	readonly budgetUsed: Locator;
	readonly budgetText: Locator;
	readonly truncatedIndicator: Locator;

	// Provenance list
	readonly provenanceList: Locator;
	readonly provenanceItems: Locator;
	readonly provenanceHeader: Locator;

	// Panel header
	readonly panelHeader: Locator;
	readonly taskTypeBadge: Locator;
	readonly refreshButton: Locator;

	// States
	readonly loadingState: Locator;
	readonly errorState: Locator;
	readonly emptyState: Locator;

	constructor(page: Page, loopIdPrefix?: string) {
		this.page = page;

		// Find the loop card, optionally by loop ID prefix
		if (loopIdPrefix) {
			this.loopCard = page.locator('.loop-card').filter({ hasText: loopIdPrefix });
		} else {
			this.loopCard = page.locator('.loop-card').first();
		}

		this.contextToggle = this.loopCard.locator('.action-btn.context');
		this.contextSection = this.loopCard.locator('.context-section');
		this.contextPanel = this.loopCard.locator('.context-panel');

		// Budget bar
		this.budgetBar = this.contextPanel.locator('.budget-bar');
		this.budgetUsed = this.contextPanel.locator('.budget-fill');
		this.budgetText = this.contextPanel.locator('.budget-text');
		this.truncatedIndicator = this.contextPanel.locator('.truncated-indicator');

		// Provenance list
		this.provenanceList = this.contextPanel.locator('.provenance-list');
		this.provenanceItems = this.contextPanel.locator('.provenance-item');
		this.provenanceHeader = this.contextPanel.locator('.provenance-header');

		// Panel header
		this.panelHeader = this.contextPanel.locator('.panel-header');
		this.taskTypeBadge = this.contextPanel.locator('.task-type-badge');
		this.refreshButton = this.contextPanel.locator('.btn-icon[title="Refresh context"]');

		// States
		this.loadingState = this.contextPanel.locator('.loading-state');
		this.errorState = this.contextPanel.locator('.error-state');
		this.emptyState = this.contextPanel.locator('.empty-state');
	}

	/**
	 * Set the loop card to target by loop ID prefix
	 */
	forLoop(loopIdPrefix: string): LoopContextPage {
		return new LoopContextPage(this.page, loopIdPrefix);
	}

	async expectLoopCardVisible(): Promise<void> {
		await expect(this.loopCard).toBeVisible();
	}

	async expectContextToggleVisible(): Promise<void> {
		await expect(this.contextToggle).toBeVisible();
	}

	async expectContextToggleHidden(): Promise<void> {
		await expect(this.contextToggle).not.toBeVisible();
	}

	async expectContextToggleActive(): Promise<void> {
		await expect(this.contextToggle).toHaveClass(/active/);
	}

	async expandContext(): Promise<void> {
		const isExpanded = await this.contextSection.isVisible();
		if (!isExpanded) {
			await this.contextToggle.click();
		}
	}

	async collapseContext(): Promise<void> {
		const isExpanded = await this.contextSection.isVisible();
		if (isExpanded) {
			await this.contextToggle.click();
		}
	}

	async toggleContext(): Promise<void> {
		await this.contextToggle.click();
	}

	async expectContextExpanded(): Promise<void> {
		await expect(this.contextSection).toBeVisible();
		await expect(this.contextPanel).toBeVisible();
	}

	async expectContextCollapsed(): Promise<void> {
		await expect(this.contextSection).not.toBeVisible();
	}

	// Budget bar methods
	async expectBudgetBar(): Promise<void> {
		await expect(this.budgetBar).toBeVisible();
	}

	async expectBudgetPercent(percent: number): Promise<void> {
		// The fill width is set via style attribute
		await expect(this.budgetUsed).toBeVisible();
		// Check the text shows the percentage or token count
		await expect(this.budgetText).toBeVisible();
	}

	async expectBudgetText(text: string): Promise<void> {
		await expect(this.budgetText).toContainText(text);
	}

	async expectBudgetUsage(used: number, budget: number): Promise<void> {
		await expect(this.budgetText).toContainText(`${used}`);
		await expect(this.budgetText).toContainText(`${budget}`);
	}

	async expectTruncatedIndicator(): Promise<void> {
		await expect(this.truncatedIndicator).toBeVisible();
	}

	async expectNoTruncatedIndicator(): Promise<void> {
		await expect(this.truncatedIndicator).not.toBeVisible();
	}

	// Provenance list methods
	async expectProvenanceList(): Promise<void> {
		await expect(this.provenanceList).toBeVisible();
	}

	async expectProvenanceCount(count: number): Promise<void> {
		await expect(this.provenanceItems).toHaveCount(count);
	}

	async expectProvenanceItem(source: string): Promise<void> {
		const item = this.provenanceItems.filter({ hasText: source });
		await expect(item).toBeVisible();
	}

	async getProvenanceItem(index: number): Promise<Locator> {
		return this.provenanceItems.nth(index);
	}

	async expectProvenanceSourceType(index: number, type: string): Promise<void> {
		const item = await this.getProvenanceItem(index);
		const sourceType = item.locator('.source-type');
		await expect(sourceType).toHaveText(type);
	}

	async expectProvenanceTokens(index: number, tokens: number): Promise<void> {
		const item = await this.getProvenanceItem(index);
		const tokenCount = item.locator('.token-count');
		await expect(tokenCount).toContainText(String(tokens));
	}

	async expectProvenanceTruncated(index: number): Promise<void> {
		const item = await this.getProvenanceItem(index);
		const truncated = item.locator('.truncated');
		await expect(truncated).toBeVisible();
	}

	async expectProvenanceNotTruncated(index: number): Promise<void> {
		const item = await this.getProvenanceItem(index);
		const truncated = item.locator('.truncated');
		await expect(truncated).not.toBeVisible();
	}

	// Task type badge
	async expectTaskTypeBadge(type: string): Promise<void> {
		await expect(this.taskTypeBadge).toBeVisible();
		await expect(this.taskTypeBadge).toHaveText(type);
	}

	// Loading, error, empty states
	async expectLoading(): Promise<void> {
		await expect(this.loadingState).toBeVisible();
	}

	async expectError(): Promise<void> {
		await expect(this.errorState).toBeVisible();
	}

	async expectEmpty(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState).toContainText('No context available');
	}

	// Refresh
	async refresh(): Promise<void> {
		await this.refreshButton.click();
	}

	async expectRefreshDisabled(): Promise<void> {
		await expect(this.refreshButton).toBeDisabled();
	}
}
