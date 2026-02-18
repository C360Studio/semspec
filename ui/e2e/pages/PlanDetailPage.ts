import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Plan Detail page.
 *
 * Provides methods to interact with and verify:
 * - Plan information and metadata
 * - Agent Pipeline View (stages, progress, parallel branches)
 * - Review Dashboard (spec gate, reviewer cards, findings)
 */
export class PlanDetailPage {
	readonly page: Page;
	readonly planDetail: Locator;
	readonly backLink: Locator;
	readonly planTitle: Locator;
	readonly planStage: Locator;
	readonly notFound: Locator;

	// Pipeline section
	readonly pipelineSection: Locator;
	readonly pipelineIndicator: Locator;
	readonly agentPipelineView: Locator;
	readonly pipelineStages: Locator;
	readonly mainPipeline: Locator;
	readonly reviewBranch: Locator;

	// Reviews section
	readonly reviewsSection: Locator;
	readonly reviewsToggle: Locator;
	readonly reviewsContent: Locator;
	readonly reviewDashboard: Locator;
	readonly specGate: Locator;
	readonly reviewerCards: Locator;
	readonly findingsList: Locator;
	readonly findingsRows: Locator;

	// Action banners
	readonly promoteBanner: Locator;
	readonly generateTasksBanner: Locator;
	readonly executeBanner: Locator;

	constructor(page: Page) {
		this.page = page;
		this.planDetail = page.locator('.plan-detail');
		this.backLink = page.locator('.back-link');
		this.planTitle = page.locator('.plan-title');
		this.planStage = page.locator('.plan-stage');
		this.notFound = page.locator('.not-found');

		// Pipeline section
		this.pipelineSection = page.locator('.pipeline-section');
		this.pipelineIndicator = page.locator('.pipeline-indicator');
		this.agentPipelineView = page.locator('.pipeline-view');
		this.pipelineStages = page.locator('.pipeline-stage');
		this.mainPipeline = page.locator('.main-pipeline');
		this.reviewBranch = page.locator('.review-branch');

		// Reviews section
		this.reviewsSection = page.locator('.reviews-section');
		this.reviewsToggle = page.locator('.reviews-toggle');
		this.reviewsContent = page.locator('.reviews-content');
		this.reviewDashboard = page.locator('.review-dashboard');
		this.specGate = page.locator('.spec-gate');
		this.reviewerCards = page.locator('.reviewer-card');
		this.findingsList = page.locator('.findings-list');
		this.findingsRows = page.locator('.finding-row');

		// Action banners
		this.promoteBanner = page.locator('.action-banner.promote');
		this.generateTasksBanner = page.locator('.action-banner.generate');
		this.executeBanner = page.locator('.action-banner.execute');
	}

	async goto(slug: string): Promise<void> {
		await this.page.goto(`/plans/${slug}`);
		// Wait for either the plan info (successful load) or not-found message
		await this.page.waitForSelector('.plan-info, .not-found', { timeout: 15000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.page.locator('.plan-info')).toBeVisible();
	}

	async expectNotFound(): Promise<void> {
		await expect(this.notFound).toBeVisible();
	}

	async expectPlanTitle(title: string): Promise<void> {
		await expect(this.planTitle).toHaveText(title);
	}

	async expectPlanStage(stage: string): Promise<void> {
		await expect(this.planStage).toHaveText(stage);
	}

	// Pipeline methods
	async expectPipelineVisible(): Promise<void> {
		await expect(this.pipelineSection).toBeVisible();
		await expect(this.agentPipelineView).toBeVisible();
	}

	async expectPipelineStageCount(count: number): Promise<void> {
		await expect(this.pipelineStages).toHaveCount(count);
	}

	async getStage(stageId: string): Promise<Locator> {
		// Stage ID is used to find pipeline stages by their data attributes or labels
		return this.pipelineStages.filter({ hasText: stageId });
	}

	async expectStageState(stageId: string, state: 'pending' | 'active' | 'complete' | 'failed'): Promise<void> {
		const stage = await this.getStage(stageId);
		await expect(stage).toHaveClass(new RegExp(state));
	}

	async expectActiveStageSpinner(stageId: string): Promise<void> {
		const stage = await this.getStage(stageId);
		const spinner = stage.locator('.spin');
		await expect(spinner).toBeVisible();
	}

	async expectStageProgress(stageId: string, current: number, max: number): Promise<void> {
		const stage = await this.getStage(stageId);
		const progress = stage.locator('.stage-progress');
		await expect(progress).toHaveText(`${current}/${max}`);
	}

	async expectReviewBranchVisible(): Promise<void> {
		await expect(this.reviewBranch).toBeVisible();
	}

	async expectReviewBranchHidden(): Promise<void> {
		await expect(this.reviewBranch).not.toBeVisible();
	}

	// Reviews methods
	async expectReviewsSectionVisible(): Promise<void> {
		await expect(this.reviewsSection).toBeVisible();
	}

	async expectReviewsSectionHidden(): Promise<void> {
		await expect(this.reviewsSection).not.toBeVisible();
	}

	async expandReviews(): Promise<void> {
		const isExpanded = await this.reviewsContent.isVisible();
		if (!isExpanded) {
			await this.reviewsToggle.click();
		}
	}

	async collapseReviews(): Promise<void> {
		const isExpanded = await this.reviewsContent.isVisible();
		if (isExpanded) {
			await this.reviewsToggle.click();
		}
	}

	async expectReviewsExpanded(): Promise<void> {
		await expect(this.reviewsContent).toBeVisible();
		await expect(this.reviewDashboard).toBeVisible();
	}

	async expectReviewsCollapsed(): Promise<void> {
		await expect(this.reviewsContent).not.toBeVisible();
	}

	async expectSpecGateVisible(): Promise<void> {
		await expect(this.specGate).toBeVisible();
	}

	async expectSpecGatePassed(): Promise<void> {
		await expect(this.specGate).toHaveClass(/passed/);
	}

	async expectSpecGateFailed(): Promise<void> {
		await expect(this.specGate).toHaveClass(/failed/);
	}

	async expectSpecGateVerdict(verdict: string): Promise<void> {
		const badge = this.specGate.locator('.badge');
		await expect(badge).toContainText(verdict);
	}

	async expectSpecGateStatus(status: 'Gate Passed' | 'Gate Failed' | 'Awaiting review'): Promise<void> {
		const statusEl = this.specGate.locator('.gate-status');
		await expect(statusEl).toContainText(status);
	}

	async expectReviewerCount(count: number): Promise<void> {
		await expect(this.reviewerCards).toHaveCount(count);
	}

	async getReviewerCard(role: string): Promise<Locator> {
		return this.reviewerCards.filter({ hasText: role });
	}

	async expectReviewerPassed(role: string): Promise<void> {
		const card = await this.getReviewerCard(role);
		await expect(card).toHaveClass(/passed/);
	}

	async expectReviewerFailed(role: string): Promise<void> {
		const card = await this.getReviewerCard(role);
		await expect(card).toHaveClass(/failed/);
	}

	async expectFindingsCount(count: number): Promise<void> {
		await expect(this.findingsRows).toHaveCount(count);
	}

	async expectFindingsListVisible(): Promise<void> {
		await expect(this.findingsList).toBeVisible();
	}

	async expectFindingSeverity(index: number, severity: string): Promise<void> {
		const finding = this.findingsRows.nth(index);
		const severityBadge = finding.locator('.severity-badge');
		await expect(severityBadge).toHaveText(severity);
	}

	async expectFindingFile(index: number, file: string): Promise<void> {
		const finding = this.findingsRows.nth(index);
		const fileEl = finding.locator('.finding-file');
		await expect(fileEl).toContainText(file);
	}

	async expectEmptyReviews(): Promise<void> {
		const emptyState = this.reviewDashboard.locator('.empty-state');
		await expect(emptyState).toBeVisible();
		await expect(emptyState).toContainText('No review results available');
	}

	async expectLoadingReviews(): Promise<void> {
		const loadingState = this.reviewDashboard.locator('.loading-state');
		await expect(loadingState).toBeVisible();
	}

	async expectReviewError(): Promise<void> {
		const errorState = this.reviewDashboard.locator('.error-state');
		await expect(errorState).toBeVisible();
	}

	// Dashboard stats
	async expectReviewerStats(passed: number, total: number): Promise<void> {
		const passCount = this.reviewDashboard.locator('.pass-count');
		await expect(passCount).toHaveText(`${passed}/${total} passed`);
	}

	async expectVerdictBadge(verdict: string): Promise<void> {
		const badge = this.reviewDashboard.locator('.dashboard-header .badge');
		await expect(badge).toContainText(verdict);
	}

	// Action banners
	async expectPromoteBannerVisible(): Promise<void> {
		await expect(this.promoteBanner).toBeVisible();
	}

	async expectGenerateTasksBannerVisible(): Promise<void> {
		await expect(this.generateTasksBanner).toBeVisible();
	}

	async expectExecuteBannerVisible(): Promise<void> {
		await expect(this.executeBanner).toBeVisible();
	}

	async clickPromote(): Promise<void> {
		await this.promoteBanner.locator('button').click();
	}

	async clickGenerateTasks(): Promise<void> {
		await this.generateTasksBanner.locator('button').click();
	}

	async clickExecute(): Promise<void> {
		await this.executeBanner.locator('button').click();
	}

	async goBack(): Promise<void> {
		await this.backLink.click();
	}
}
