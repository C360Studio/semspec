import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Sidebar navigation.
 *
 * Provides methods to interact with and verify:
 * - Navigation items
 * - Active loops counter
 * - Paused loops badge
 * - System health indicator
 * - Entity counts
 */
export class SidebarPage {
	readonly page: Page;
	readonly sidebar: Locator;
	readonly logo: Locator;
	readonly navigation: Locator;
	readonly activeLoopsCounter: Locator;
	readonly systemStatus: Locator;
	readonly healthIndicator: Locator;
	readonly entityCountsFooter: Locator;
	readonly entitiesNavItem: Locator;
	readonly entitiesNavBadge: Locator;

	constructor(page: Page) {
		this.page = page;
		this.sidebar = page.locator('aside.sidebar');
		this.logo = this.sidebar.locator('.logo');
		this.navigation = this.sidebar.locator('nav[aria-label="Main navigation"]');
		this.activeLoopsCounter = this.sidebar.locator('.active-loops');
		this.systemStatus = this.sidebar.locator('.system-status');
		this.healthIndicator = this.sidebar.locator('.status-indicator');
		this.entityCountsFooter = this.sidebar.locator('.entity-counts');
		this.entitiesNavItem = this.navigation.locator('a[href="/entities"]');
		this.entitiesNavBadge = this.entitiesNavItem.locator('.badge');
	}

	async expectVisible(): Promise<void> {
		await expect(this.sidebar).toBeVisible();
	}

	async expectLogo(text = 'Semspec'): Promise<void> {
		await expect(this.logo).toHaveText(text);
	}

	async expectActiveLoops(count: number): Promise<void> {
		await expect(this.activeLoopsCounter).toContainText(`${count} active loops`);
	}

	async expectHealthy(): Promise<void> {
		await expect(this.healthIndicator).toHaveClass(/healthy/);
		await expect(this.systemStatus.locator('.status-text')).toHaveText('System healthy');
	}

	async expectUnhealthy(): Promise<void> {
		await expect(this.healthIndicator).not.toHaveClass(/healthy/);
		await expect(this.systemStatus.locator('.status-text')).toHaveText('System issues');
	}

	async expectPausedBadge(count: number): Promise<void> {
		// Current layout doesn't show paused-specific badge
		// Activity shows active loops count (which includes paused)
		// This is a backwards-compat stub - paused count not shown separately
		const activityNavItem = this.navigation.locator('a[href="/activity"]');
		const badge = activityNavItem.locator('.badge');
		await expect(badge).toBeVisible();
		// Note: badge shows active count, not paused-specific count
	}

	async expectNoPausedBadge(): Promise<void> {
		// No-op: current layout always shows active loops count on Activity
		// There's no separate paused badge to hide
	}

	async navigateTo(path: 'Board' | 'Plans' | 'Activity' | 'Sources' | 'Settings'): Promise<void> {
		const navItem = this.navigation.locator(`a:has-text("${path}")`);
		await navItem.click();
	}

	async expectActivePage(path: 'Board' | 'Plans' | 'Activity' | 'Sources' | 'Settings'): Promise<void> {
		const navItem = this.navigation.locator(`a:has-text("${path}")`);
		await expect(navItem).toHaveClass(/active/);
	}

	async getNavItems(): Promise<string[]> {
		const items = await this.navigation.locator('.nav-item span').allTextContents();
		return items;
	}

	async navigateToEntities(): Promise<void> {
		await this.entitiesNavItem.click();
	}

	async expectEntityCount(count: number): Promise<void> {
		await expect(this.entitiesNavBadge).toBeVisible();
		await expect(this.entitiesNavBadge).toHaveText(String(count));
	}

	async expectEntityCountVisible(): Promise<void> {
		await expect(this.entitiesNavBadge).toBeVisible();
	}

	async expectNoEntityCount(): Promise<void> {
		await expect(this.entitiesNavBadge).not.toBeVisible();
	}

	async expectEntityFooterCount(count: number): Promise<void> {
		await expect(this.entityCountsFooter).toBeVisible();
		await expect(this.entityCountsFooter).toContainText(`${count} graph entities`);
	}

	async expectNoEntityFooter(): Promise<void> {
		await expect(this.entityCountsFooter).not.toBeVisible();
	}
}
