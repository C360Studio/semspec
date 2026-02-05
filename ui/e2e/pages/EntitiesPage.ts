import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Entity Browser.
 *
 * Provides methods to interact with and verify:
 * - Entity list and filtering
 * - Search functionality
 * - Type filtering
 * - Entity detail navigation
 * - Empty, loading, and error states
 */
export class EntitiesPage {
	readonly page: Page;
	readonly pageHeader: Locator;
	readonly searchInput: Locator;
	readonly typeFilter: Locator;
	readonly entityList: Locator;
	readonly entityCards: Locator;
	readonly loadingState: Locator;
	readonly errorState: Locator;
	readonly emptyState: Locator;
	readonly retryButton: Locator;

	constructor(page: Page) {
		this.page = page;
		this.pageHeader = page.locator('.page-header h1');
		this.searchInput = page.locator('input[aria-label="Search entities"]');
		this.typeFilter = page.locator('select[aria-label="Filter by type"]');
		this.entityList = page.locator('.entity-list');
		this.entityCards = page.locator('.entity-card');
		this.loadingState = page.locator('.loading-state');
		this.errorState = page.locator('.error-state');
		this.emptyState = page.locator('.empty-state');
		this.retryButton = page.locator('.error-state button');
	}

	async goto(): Promise<void> {
		await this.page.goto('/entities');
		await expect(this.pageHeader).toBeVisible();
		// Wait for initial data load
		await this.waitForDataLoad();
	}

	async search(query: string): Promise<void> {
		await this.searchInput.fill(query);
		// Wait for debounced search to trigger (300ms) plus buffer
		await this.page.waitForTimeout(500);
		// Wait for loading state to disappear
		await this.waitForDataLoad();
	}

	async clearSearch(): Promise<void> {
		await this.searchInput.clear();
		// Wait for debounced search to trigger
		await this.page.waitForTimeout(500);
		// Wait for loading state to disappear
		await this.waitForDataLoad();
	}

	async filterByType(type: 'All Types' | 'Code' | 'Proposals' | 'Specifications' | 'Tasks' | 'Loops' | 'Activities'): Promise<void> {
		// Map display labels to form values
		const valueMap: Record<string, string> = {
			'All Types': '',
			'Code': 'code',
			'Proposals': 'proposal',
			'Specifications': 'spec',
			'Tasks': 'task',
			'Loops': 'loop',
			'Activities': 'activity'
		};
		await this.typeFilter.selectOption(valueMap[type]);
		// Wait for data to load
		await this.waitForDataLoad();
	}

	private async waitForDataLoad(): Promise<void> {
		// Wait for loading indicator to appear then disappear, or just confirm no loading
		try {
			await this.loadingState.waitFor({ state: 'visible', timeout: 1000 });
		} catch {
			// Loading might already be done or very fast
		}
		await this.loadingState.waitFor({ state: 'hidden', timeout: 10000 });
	}

	async expectPageTitle(title = 'Entity Browser'): Promise<void> {
		await expect(this.pageHeader).toHaveText(title);
	}

	async expectEntityCount(count: number): Promise<void> {
		await expect(this.entityCards).toHaveCount(count);
	}

	async expectMinimumEntityCount(min: number): Promise<void> {
		const count = await this.entityCards.count();
		expect(count).toBeGreaterThanOrEqual(min);
	}

	async getEntityCount(): Promise<number> {
		return this.entityCards.count();
	}

	async expectLoading(): Promise<void> {
		await expect(this.loadingState).toBeVisible();
	}

	async expectNoLoading(): Promise<void> {
		await expect(this.loadingState).not.toBeVisible();
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState.locator('h2')).toHaveText('No entities found');
	}

	async expectEmptyStateWithFilters(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState.locator('p')).toContainText('Try adjusting your search or filters');
	}

	async expectEmptyStateNoData(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState.locator('p')).toContainText('knowledge graph is empty');
	}

	async expectErrorState(): Promise<void> {
		await expect(this.errorState).toBeVisible();
	}

	async expectErrorMessage(message: string): Promise<void> {
		await expect(this.errorState).toContainText(message);
	}

	async clickRetry(): Promise<void> {
		await this.retryButton.click();
	}

	async clickEntity(name: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name }).first();
		await card.click();
	}

	async clickEntityByIndex(index: number): Promise<void> {
		await this.entityCards.nth(index).click();
	}

	async expectEntityVisible(name: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name });
		await expect(card).toBeVisible();
	}

	async expectEntityNotVisible(name: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name });
		await expect(card).not.toBeVisible();
	}

	async expectEntityHasTypeBadge(name: string, type: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name });
		const typeBadge = card.locator('.entity-type-badge');
		// The badge displays lowercase text with CSS text-transform: uppercase
		await expect(typeBadge).toHaveText(type.toLowerCase());
	}

	async expectEntityHasStatusBadge(name: string, status: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name });
		const statusBadge = card.locator('.status-badge');
		await expect(statusBadge).toHaveText(status);
	}

	async expectEntityHasPath(name: string, path: string): Promise<void> {
		const card = this.entityCards.filter({ hasText: name });
		const pathInfo = card.locator('.path-info');
		await expect(pathInfo).toHaveText(path);
	}

	async getEntityNames(): Promise<string[]> {
		const names = await this.entityCards.locator('.entity-name').allTextContents();
		return names;
	}

	async getEntityTypes(): Promise<string[]> {
		const types = await this.entityCards.locator('.entity-type-badge').allTextContents();
		return types.map(t => t.toLowerCase());
	}

	async expectTypeFilterValue(value: string): Promise<void> {
		await expect(this.typeFilter).toHaveValue(value);
	}

	async expectSearchValue(value: string): Promise<void> {
		await expect(this.searchInput).toHaveValue(value);
	}
}

/**
 * Page Object Model for the Entity Detail view.
 *
 * Provides methods to interact with and verify:
 * - Entity header and metadata
 * - BFO/CCO classification badges
 * - Predicates list
 * - Relationships by category
 * - Navigation
 */
export class EntityDetailPage {
	readonly page: Page;
	readonly backButton: Locator;
	readonly entityTitle: Locator;
	readonly entityType: Locator;
	readonly entityId: Locator;
	readonly bfoBadge: Locator;
	readonly ccoBadge: Locator;
	readonly predicatesSection: Locator;
	readonly predicateRows: Locator;
	readonly provenanceSection: Locator;
	readonly structureSection: Locator;
	readonly semanticSection: Locator;
	readonly relationshipItems: Locator;
	readonly timestampsSection: Locator;
	readonly loadingState: Locator;
	readonly errorState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.backButton = page.locator('.back-button');
		this.entityTitle = page.locator('.entity-title h1');
		this.entityType = page.locator('.entity-type');
		this.entityId = page.locator('.entity-id');
		this.bfoBadge = page.locator('.bfo-badge');
		this.ccoBadge = page.locator('.cco-badge');
		this.predicatesSection = page.locator('.predicates-section');
		this.predicateRows = page.locator('.predicate-row');
		this.provenanceSection = page.locator('.relationships-section').filter({ hasText: 'Provenance' });
		this.structureSection = page.locator('.relationships-section').filter({ hasText: 'Structure' });
		this.semanticSection = page.locator('.relationships-section').filter({ hasText: 'Semantic' });
		this.relationshipItems = page.locator('.relationship-item');
		this.timestampsSection = page.locator('.timestamps-section');
		this.loadingState = page.locator('.loading-state');
		this.errorState = page.locator('.error-state');
	}

	async goto(entityId: string): Promise<void> {
		await this.page.goto(`/entities/${encodeURIComponent(entityId)}`);
	}

	async expectLoading(): Promise<void> {
		await expect(this.loadingState).toBeVisible();
	}

	async expectNoLoading(): Promise<void> {
		await expect(this.loadingState).not.toBeVisible();
	}

	async expectErrorState(): Promise<void> {
		await expect(this.errorState).toBeVisible();
	}

	async expectErrorMessage(message: string): Promise<void> {
		await expect(this.errorState).toContainText(message);
	}

	async expectEntityName(name: string): Promise<void> {
		await expect(this.entityTitle).toHaveText(name);
	}

	async expectEntityType(type: string): Promise<void> {
		await expect(this.entityType).toHaveText(type);
	}

	async expectEntityId(id: string): Promise<void> {
		await expect(this.entityId).toHaveText(id);
	}

	async expectEntityNameVisible(): Promise<void> {
		await expect(this.entityTitle).toBeVisible();
	}

	async expectEntityIdVisible(): Promise<void> {
		await expect(this.entityId).toBeVisible();
	}

	async expectBfoClassification(classification: string): Promise<void> {
		await expect(this.bfoBadge).toContainText(classification);
	}

	async expectCcoClassification(classification: string): Promise<void> {
		await expect(this.ccoBadge).toContainText(classification);
	}

	async expectPredicateCount(count: number): Promise<void> {
		await expect(this.predicateRows).toHaveCount(count);
	}

	async expectMinimumPredicateCount(min: number): Promise<void> {
		const count = await this.predicateRows.count();
		expect(count).toBeGreaterThanOrEqual(min);
	}

	async expectPredicateValue(key: string, value: string): Promise<void> {
		const row = this.predicateRows.filter({ hasText: key });
		await expect(row.locator('.predicate-value')).toContainText(value);
	}

	async expectProvenanceRelationships(): Promise<void> {
		await expect(this.provenanceSection).toBeVisible();
	}

	async expectStructureRelationships(): Promise<void> {
		await expect(this.structureSection).toBeVisible();
	}

	async expectSemanticRelationships(): Promise<void> {
		await expect(this.semanticSection).toBeVisible();
	}

	async expectNoProvenanceRelationships(): Promise<void> {
		await expect(this.provenanceSection).not.toBeVisible();
	}

	async expectRelationshipCount(count: number): Promise<void> {
		await expect(this.relationshipItems).toHaveCount(count);
	}

	async clickRelationship(targetName: string): Promise<void> {
		const rel = this.relationshipItems.filter({ hasText: targetName });
		await rel.click();
	}

	async goBack(): Promise<void> {
		// The back button uses goto('/entities') which triggers client-side navigation
		await this.backButton.click();
		// Wait for URL to no longer contain entity ID (detail page pattern)
		await this.page.waitForFunction(() => {
			return window.location.pathname === '/entities';
		}, { timeout: 10000 });
	}

	async expectBackButtonVisible(): Promise<void> {
		await expect(this.backButton).toBeVisible();
		await expect(this.backButton).toContainText('Back to Entities');
	}

	async expectTimestamps(): Promise<void> {
		await expect(this.timestampsSection).toBeVisible();
	}

	async expectCreatedAt(date: string): Promise<void> {
		const createdDiv = this.timestampsSection.locator('.timestamp').filter({ hasText: 'Created' });
		await expect(createdDiv).toContainText(date);
	}
}
