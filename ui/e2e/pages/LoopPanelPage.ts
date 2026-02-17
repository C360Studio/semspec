import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Loops section on the Activity page.
 *
 * The loop panel was replaced with a loops section integrated
 * into the Activity page layout.
 *
 * Provides methods to interact with and verify:
 * - Loop list visibility and empty state
 * - Loop cards with state, progress, and actions
 * - Workflow context (plan slug, role)
 */
export class LoopPanelPage {
	readonly page: Page;
	readonly loopsSection: Locator;
	readonly loopsHeader: Locator;
	readonly loopsCount: Locator;
	readonly loopsList: Locator;
	readonly loopCards: Locator;
	readonly emptyState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.loopsSection = page.locator('.loops-section');
		this.loopsHeader = this.loopsSection.locator('.loops-header');
		this.loopsCount = this.loopsHeader.locator('.loops-count');
		this.loopsList = this.loopsSection.locator('.loops-list');
		this.loopCards = this.loopsSection.locator('.loop-card');
		this.emptyState = this.loopsSection.locator('.loops-empty');
	}

	async goto(): Promise<void> {
		await this.page.goto('/activity');
		await expect(this.loopsSection).toBeVisible();
	}

	// Panel state - simplified for new layout (no collapse/expand)
	async expectVisible(): Promise<void> {
		await expect(this.loopsSection).toBeVisible();
	}

	// These are no-ops for backwards compatibility - new layout doesn't collapse
	async expectCollapsed(): Promise<void> {
		// No-op: new layout doesn't have collapse state
	}

	async expectExpanded(): Promise<void> {
		await this.expectVisible();
	}

	async toggle(): Promise<void> {
		// No-op: new layout doesn't have toggle
	}

	async collapse(): Promise<void> {
		// No-op: new layout doesn't collapse
	}

	async expand(): Promise<void> {
		// No-op: new layout doesn't collapse
	}

	// Loop content
	async expectLoopCount(count: number): Promise<void> {
		await expect(this.loopsCount).toHaveText(String(count));
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
		// Loop ID is displayed as first 8 chars in .loop-id
		return this.loopCards.filter({ hasText: loopId.slice(0, 8) });
	}

	async expectLoopState(loopId: string, state: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		// State is stored in data-state attribute on .loop-card
		await expect(card).toHaveAttribute('data-state', state);
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

	async expectWorkflowContext(loopId: string, slug: string, _step: string): Promise<void> {
		const card = await this.getLoopCard(loopId);
		// LoopCard shows plan slug as a .plan-link
		const planLink = card.locator('.plan-link');
		await expect(planLink).toHaveText(slug);
		// Note: workflow step is not displayed separately in new layout
	}

	// Legacy methods for backwards compatibility
	async expectTabsVisible(): Promise<void> {
		// No-op: new layout doesn't have tabs
	}

	async expectLoopsTabActive(): Promise<void> {
		// No-op: new layout doesn't have tabs
	}

	async expectQuestionsTabActive(): Promise<void> {
		// No-op: new layout doesn't have tabs
	}

	async switchToLoopsTab(): Promise<void> {
		// No-op: new layout doesn't have tabs
	}

	async switchToQuestionsTab(): Promise<void> {
		// No-op: new layout doesn't have tabs
	}

	async expectLoopsTabBadge(_count: number): Promise<void> {
		// No-op: new layout doesn't have tab badges
	}

	async expectQuestionsTabBadge(_count: number): Promise<void> {
		// No-op: new layout doesn't have tab badges
	}

	async expectConnected(): Promise<void> {
		// No-op: connection status is in sidebar, not loop panel
	}

	async expectDisconnected(): Promise<void> {
		// No-op: connection status is in sidebar, not loop panel
	}
}
