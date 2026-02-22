import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Loops section on the Activity page.
 *
 * The loop panel is now a CollapsiblePanel with id="activity-loops".
 *
 * Provides methods to interact with and verify:
 * - Loop list visibility and empty state
 * - Loop cards with state, progress, and actions
 * - Workflow context (plan slug, role)
 * - Panel collapse/expand functionality
 */
export class LoopPanelPage {
	readonly page: Page;
	readonly loopsPanel: Locator;
	readonly collapseToggle: Locator;
	readonly loopsCount: Locator;
	readonly loopsList: Locator;
	readonly loopCards: Locator;
	readonly emptyState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.loopsPanel = page.locator('[data-panel-id="activity-loops"]');
		this.collapseToggle = this.loopsPanel.locator('.collapse-toggle');
		this.loopsCount = this.loopsPanel.locator('.loops-count');
		this.loopsList = this.loopsPanel.locator('.loops-list');
		this.loopCards = this.loopsPanel.locator('.loop-card');
		this.emptyState = this.loopsPanel.locator('.loops-empty');
	}

	async goto(): Promise<void> {
		await this.page.goto('/activity');
		await expect(this.loopsPanel).toBeVisible();
	}

	// Panel state
	async expectVisible(): Promise<void> {
		await expect(this.loopsPanel).toBeVisible();
	}

	async expectCollapsed(): Promise<void> {
		await expect(this.loopsPanel).toHaveClass(/collapsed/);
	}

	async expectExpanded(): Promise<void> {
		await expect(this.loopsPanel).not.toHaveClass(/collapsed/);
	}

	async toggle(): Promise<void> {
		await this.collapseToggle.waitFor({ state: 'visible' });
		await this.collapseToggle.click({ timeout: 5000 });
	}

	async collapse(): Promise<void> {
		// Check if already collapsed before toggling
		const classList = await this.loopsPanel.getAttribute('class');
		if (classList && !classList.includes('collapsed')) {
			await this.toggle();
		}
	}

	async expand(): Promise<void> {
		// Check if collapsed before toggling
		const classList = await this.loopsPanel.getAttribute('class');
		if (classList && classList.includes('collapsed')) {
			await this.toggle();
		}
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
		// LoopCard displays first 8 chars of loop_id
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
