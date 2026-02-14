import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Activity page.
 *
 * Provides methods to interact with and verify:
 * - View toggle (feed/timeline)
 * - Activity Feed
 * - Agent Timeline (tracks, segments, legend)
 * - Active loops section
 */
export class ActivityPage {
	readonly page: Page;
	readonly activityView: Locator;
	readonly activityLeft: Locator;
	readonly activityRight: Locator;

	// View toggle
	readonly viewToggle: Locator;
	readonly feedToggle: Locator;
	readonly timelineToggle: Locator;

	// Feed section
	readonly feedSection: Locator;
	readonly activityFeed: Locator;

	// Timeline section
	readonly timelineSection: Locator;
	readonly agentTimeline: Locator;
	readonly timelineHeader: Locator;
	readonly timelineTracks: Locator;
	readonly timelineSegments: Locator;
	readonly timelineLegend: Locator;
	readonly timeAxis: Locator;
	readonly liveIndicator: Locator;
	readonly durationBadge: Locator;
	readonly segmentDetails: Locator;
	readonly emptyState: Locator;

	// Loops section
	readonly loopsSection: Locator;
	readonly loopsHeader: Locator;
	readonly loopsList: Locator;
	readonly loopCards: Locator;
	readonly loopsEmpty: Locator;
	readonly loopsCount: Locator;

	// Questions and chat
	readonly questionsSection: Locator;
	readonly chatSection: Locator;

	constructor(page: Page) {
		this.page = page;
		this.activityView = page.locator('.activity-view');
		this.activityLeft = page.locator('.activity-left');
		this.activityRight = page.locator('.activity-right');

		// View toggle
		this.viewToggle = page.locator('.view-toggle');
		this.feedToggle = this.viewToggle.locator('.toggle-btn').filter({ hasText: 'Feed' });
		this.timelineToggle = this.viewToggle.locator('.toggle-btn').filter({ hasText: 'Timeline' });

		// Feed section
		this.feedSection = page.locator('.feed-section');
		this.activityFeed = page.locator('.activity-feed');

		// Timeline section
		this.timelineSection = page.locator('.timeline-section');
		this.agentTimeline = page.locator('.agent-timeline');
		this.timelineHeader = this.agentTimeline.locator('.timeline-header');
		this.timelineTracks = page.locator('.timeline-track');
		this.timelineSegments = page.locator('.timeline-segment');
		this.timelineLegend = this.agentTimeline.locator('.timeline-legend');
		this.timeAxis = this.agentTimeline.locator('.time-axis');
		this.liveIndicator = this.agentTimeline.locator('.live-indicator');
		this.durationBadge = this.agentTimeline.locator('.duration-badge');
		this.segmentDetails = this.agentTimeline.locator('.segment-details');
		this.emptyState = this.agentTimeline.locator('.empty-state');

		// Loops section
		this.loopsSection = page.locator('.loops-section');
		this.loopsHeader = page.locator('.loops-header');
		this.loopsList = page.locator('.loops-list');
		this.loopCards = page.locator('.loop-card');
		this.loopsEmpty = page.locator('.loops-empty');
		this.loopsCount = page.locator('.loops-count');

		// Questions and chat
		this.questionsSection = page.locator('.questions-section');
		this.chatSection = page.locator('.chat-section');
	}

	async goto(): Promise<void> {
		await this.page.goto('/activity');
		await expect(this.activityView).toBeVisible();
	}

	async expectVisible(): Promise<void> {
		await expect(this.activityView).toBeVisible();
	}

	// View mode
	async expectFeedView(): Promise<void> {
		await expect(this.feedToggle).toHaveClass(/active/);
		await expect(this.feedSection).toBeVisible();
		await expect(this.timelineSection).not.toBeVisible();
	}

	async expectTimelineView(): Promise<void> {
		await expect(this.timelineToggle).toHaveClass(/active/);
		await expect(this.timelineSection).toBeVisible();
		await expect(this.feedSection).not.toBeVisible();
	}

	async switchToTimeline(): Promise<void> {
		await this.timelineToggle.click();
		// Wait for reactivity - give Svelte time to update the DOM
		await this.page.waitForTimeout(100);
		// The timeline view has a unique "Agent Timeline" heading
		await this.page.waitForSelector('h3:has-text("Agent Timeline")', { timeout: 5000 });
	}

	async switchToFeed(): Promise<void> {
		await this.feedToggle.click();
		// Wait for reactivity
		await this.page.waitForTimeout(100);
		// The feed view has "Activity Feed" heading
		await this.page.waitForSelector('h2:has-text("Activity Feed")', { timeout: 5000 });
	}

	// Timeline methods
	async expectTimelineVisible(): Promise<void> {
		await expect(this.agentTimeline).toBeVisible();
	}

	async expectTimelineEmpty(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState).toContainText('No agent activity to display');
	}

	async expectTrackCount(count: number): Promise<void> {
		await expect(this.timelineTracks).toHaveCount(count);
	}

	async getTrack(role: string): Promise<Locator> {
		return this.timelineTracks.filter({ hasText: role });
	}

	async expectTrackLabel(trackIndex: number, label: string): Promise<void> {
		const track = this.timelineTracks.nth(trackIndex);
		const trackLabel = track.locator('.track-label');
		await expect(trackLabel).toHaveText(label);
	}

	async expectSegmentCount(count: number): Promise<void> {
		await expect(this.timelineSegments).toHaveCount(count);
	}

	async getSegment(loopIdPrefix: string): Promise<Locator> {
		// Find segment by partial loop ID
		return this.timelineSegments.filter({ hasText: loopIdPrefix });
	}

	async expectSegmentState(index: number, state: 'active' | 'complete' | 'waiting' | 'blocked' | 'failed'): Promise<void> {
		const segment = this.timelineSegments.nth(index);
		await expect(segment).toHaveClass(new RegExp(state));
	}

	async clickSegment(index: number): Promise<void> {
		await this.timelineSegments.nth(index).click();
	}

	async expectSegmentDetails(): Promise<void> {
		await expect(this.segmentDetails).toBeVisible();
	}

	async expectSegmentDetailsHidden(): Promise<void> {
		await expect(this.segmentDetails).not.toBeVisible();
	}

	async closeSegmentDetails(): Promise<void> {
		await this.segmentDetails.locator('.close-btn').click();
	}

	async expectSegmentLoopId(loopIdPrefix: string): Promise<void> {
		const loopIdRow = this.segmentDetails.locator('.detail-row').filter({ hasText: 'Loop ID' });
		await expect(loopIdRow).toContainText(loopIdPrefix);
	}

	async expectSegmentDetailState(state: string): Promise<void> {
		const stateRow = this.segmentDetails.locator('.detail-row').filter({ hasText: 'State' });
		await expect(stateRow).toContainText(state);
	}

	async expectSegmentDetailProgress(current: number, max: number): Promise<void> {
		const progressRow = this.segmentDetails.locator('.detail-row').filter({ hasText: 'Progress' });
		await expect(progressRow).toContainText(`${current}/${max}`);
	}

	// Live indicator
	async expectLiveIndicator(): Promise<void> {
		await expect(this.liveIndicator).toBeVisible();
		await expect(this.liveIndicator).toContainText('Live');
	}

	async expectNoLiveIndicator(): Promise<void> {
		await expect(this.liveIndicator).not.toBeVisible();
	}

	async expectLiveDotPulsing(): Promise<void> {
		const liveDot = this.liveIndicator.locator('.live-dot');
		await expect(liveDot).toBeVisible();
		// The pulsing animation is CSS-based, just check the element exists
	}

	// Time axis
	async expectTimeAxisVisible(): Promise<void> {
		await expect(this.timeAxis).toBeVisible();
	}

	async expectTimeAxisHidden(): Promise<void> {
		await expect(this.timeAxis).not.toBeVisible();
	}

	async expectTimeLabel(offset: number): Promise<void> {
		const label = this.timeAxis.locator('.time-label').nth(offset);
		await expect(label).toBeVisible();
	}

	// Duration badge
	async expectDuration(duration: string): Promise<void> {
		await expect(this.durationBadge).toContainText(duration);
	}

	// Legend
	async expectLegendVisible(): Promise<void> {
		await expect(this.timelineLegend).toBeVisible();
	}

	async expectLegendItems(): Promise<void> {
		const items = this.timelineLegend.locator('.legend-item');
		await expect(items).toHaveCount(5); // Active, Complete, Waiting, Blocked, Failed
	}

	async expectLegendItem(state: 'Active' | 'Complete' | 'Waiting' | 'Blocked' | 'Failed'): Promise<void> {
		const item = this.timelineLegend.locator('.legend-item').filter({ hasText: state });
		await expect(item).toBeVisible();
	}

	// Loops section
	async expectLoopsEmpty(): Promise<void> {
		await expect(this.loopsEmpty).toBeVisible();
	}

	async expectActiveLoopCount(count: number): Promise<void> {
		await expect(this.loopsCount).toHaveText(String(count));
	}

	async expectLoopCardCount(count: number): Promise<void> {
		await expect(this.loopCards).toHaveCount(count);
	}

	async getLoopCard(loopIdSuffix: string): Promise<Locator> {
		return this.loopCards.filter({ hasText: loopIdSuffix });
	}

	async expectLoopState(loopIdSuffix: string, state: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await expect(card).toHaveAttribute('data-state', state);
	}

	async pauseLoop(loopIdSuffix: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await card.locator('.loop-btn[title="Pause"]').click();
	}

	async resumeLoop(loopIdSuffix: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await card.locator('.loop-btn[title="Resume"]').click();
	}

	async cancelLoop(loopIdSuffix: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await card.locator('.loop-btn[title="Cancel"]').click();
	}
}
