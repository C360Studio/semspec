import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Loop Panel.
 *
 * Provides methods to interact with and verify:
 * - Panel visibility and collapse state
 * - Active loops list
 * - Loop cards with state, progress, and actions
 * - Connection status
 */
export class LoopPanelPage {
	readonly page: Page;
	readonly panel: Locator;
	readonly toggleButton: Locator;
	readonly header: Locator;
	readonly loopCount: Locator;
	readonly loopList: Locator;
	readonly loopCards: Locator;
	readonly emptyState: Locator;
	readonly loadingState: Locator;
	readonly connectionStatus: Locator;

	constructor(page: Page) {
		this.page = page;
		this.panel = page.locator('aside.loop-panel');
		this.toggleButton = this.panel.locator('.panel-toggle');
		this.header = this.panel.locator('.panel-header');
		this.loopCount = this.header.locator('.loop-count');
		this.loopList = this.panel.locator('.loop-list');
		this.loopCards = this.panel.locator('.loop-card');
		this.emptyState = this.panel.locator('.empty-state');
		this.loadingState = this.panel.locator('.loading-state');
		this.connectionStatus = this.panel.locator('.connection-status');
	}

	async expectVisible(): Promise<void> {
		await expect(this.panel).toBeVisible();
	}

	async expectCollapsed(): Promise<void> {
		await expect(this.panel).toHaveClass(/collapsed/);
	}

	async expectExpanded(): Promise<void> {
		await expect(this.panel).not.toHaveClass(/collapsed/);
	}

	async toggle(): Promise<void> {
		await this.toggleButton.click();
	}

	async collapse(): Promise<void> {
		const isCollapsed = await this.panel.evaluate(el => el.classList.contains('collapsed'));
		if (!isCollapsed) {
			await this.toggle();
		}
	}

	async expand(): Promise<void> {
		const isCollapsed = await this.panel.evaluate(el => el.classList.contains('collapsed'));
		if (isCollapsed) {
			await this.toggle();
		}
	}

	async expectLoopCount(count: number): Promise<void> {
		await expect(this.loopCount).toHaveText(String(count));
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
	}

	async expectNoEmptyState(): Promise<void> {
		await expect(this.emptyState).not.toBeVisible();
	}

	async expectLoopCards(count: number): Promise<void> {
		await expect(this.loopCards).toHaveCount(count);
	}

	async getLoopCard(loopId: string): Promise<Locator> {
		return this.loopCards.filter({ hasText: loopId.slice(0, 8) });
	}

	async expectLoopState(loopId: string, state: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const stateBadge = card.locator('.state-badge');
		await expect(stateBadge).toHaveText(state);
	}

	async expectLoopProgress(loopId: string, current: number, max: number): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const progressText = card.locator('.progress-text');
		await expect(progressText).toHaveText(`${current}/${max}`);
	}

	async pauseLoop(loopId: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const pauseButton = card.locator('.action-btn.pause');
		await pauseButton.click();
	}

	async resumeLoop(loopId: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const resumeButton = card.locator('.action-btn.resume');
		await resumeButton.click();
	}

	async cancelLoop(loopId: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const cancelButton = card.locator('.action-btn.cancel');
		await cancelButton.click();
	}

	async expectConnected(): Promise<void> {
		await expect(this.connectionStatus).toHaveClass(/connected/);
	}

	async expectDisconnected(): Promise<void> {
		await expect(this.connectionStatus).not.toHaveClass(/connected/);
	}

	async expectWorkflowContext(loopId: string, slug: string, step: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		const workflowSlug = card.locator('.workflow-slug');
		const workflowStep = card.locator('.workflow-step');
		await expect(workflowSlug).toHaveText(slug);
		await expect(workflowStep).toHaveText(step);
	}
}
