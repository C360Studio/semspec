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
		const tasksNavItem = this.navigation.locator('a[href="/tasks"]');
		const badge = tasksNavItem.locator('.badge');
		await expect(badge).toBeVisible();
		await expect(badge).toHaveText(String(count));
	}

	async expectNoPausedBadge(): Promise<void> {
		const tasksNavItem = this.navigation.locator('a[href="/tasks"]');
		const badge = tasksNavItem.locator('.badge');
		await expect(badge).not.toBeVisible();
	}

	async navigateTo(path: 'Chat' | 'Dashboard' | 'Entities' | 'Tasks' | 'History' | 'Settings'): Promise<void> {
		const navItem = this.navigation.locator(`a:has-text("${path}")`);
		await navItem.click();
	}

	async expectActivePage(path: 'Chat' | 'Dashboard' | 'Entities' | 'Tasks' | 'History' | 'Settings'): Promise<void> {
		const navItem = this.navigation.locator(`a:has-text("${path}")`);
		await expect(navItem).toHaveAttribute('aria-current', 'page');
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
