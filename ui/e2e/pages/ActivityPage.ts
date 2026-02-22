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

	// Collapsible Panels
	readonly feedPanel: Locator;
	readonly loopsPanel: Locator;
	readonly chatPanel: Locator;

	// View toggle (inside feed panel header)
	readonly viewToggle: Locator;
	readonly feedToggle: Locator;
	readonly timelineToggle: Locator;

	// Feed section (inside feed panel)
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

		// Collapsible Panels
		this.feedPanel = page.locator('[data-panel-id="activity-feed"]');
		this.loopsPanel = page.locator('[data-panel-id="activity-loops"]');
		this.chatPanel = page.locator('[data-panel-id="activity-chat"]');

		// View toggle (inside feed panel header)
		this.viewToggle = page.locator('.view-toggle');
		this.feedToggle = this.viewToggle.locator('.toggle-btn').filter({ hasText: 'Feed' });
		this.timelineToggle = this.viewToggle.locator('.toggle-btn').filter({ hasText: 'Timeline' });

		// Feed section (inside panel body)
		this.feedSection = this.feedPanel.locator('.panel-body');
		this.activityFeed = page.locator('.activity-feed');

		// Timeline section (inside panel body when timeline view is active)
		this.timelineSection = page.locator('.timeline-content');
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

		// Loops section (inside loops panel)
		this.loopsSection = this.loopsPanel;
		this.loopsHeader = this.loopsPanel.locator('.panel-header');
		this.loopsList = page.locator('.loops-list');
		this.loopCards = page.locator('.loop-card');
		this.loopsEmpty = page.locator('.loops-empty');
		this.loopsCount = page.locator('.loops-count');

		// Questions and chat (inside chat panel)
		this.questionsSection = page.locator('.questions-section');
		this.chatSection = page.locator('.chat-section');
	}

	async goto(): Promise<void> {
		await this.page.goto('/activity');
		await expect(this.activityView).toBeVisible();
		// Wait for SvelteKit hydration to complete (body.hydrated class added in layout)
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });
	}

	async expectVisible(): Promise<void> {
		await expect(this.activityView).toBeVisible();
	}

	// View mode
	async expectFeedView(): Promise<void> {
		await expect(this.feedToggle).toHaveClass(/active/);
		// Activity Feed heading should be visible
		await expect(this.activityFeed).toBeVisible();
		// Timeline content should not be visible
		await expect(this.timelineSection).not.toBeVisible();
	}

	async expectTimelineView(): Promise<void> {
		await expect(this.timelineToggle).toHaveClass(/active/);
		// Timeline content should be visible
		await expect(this.timelineSection).toBeVisible();
		// Activity Feed should not be visible
		await expect(this.activityFeed).not.toBeVisible();
	}

	async switchToTimeline(): Promise<void> {
		// Wait for the button to be interactive (hydration may be in progress)
		await this.timelineToggle.waitFor({ state: 'visible' });

		// Click with retry - Svelte 5's {#key} block may recreate buttons during hydration
		for (let i = 0; i < 5; i++) {
			await this.timelineToggle.click({ force: true });

			// Wait a bit for Svelte reactivity to update the DOM
			try {
				await this.page.waitForSelector('h3:has-text("Agent Timeline")', { timeout: 3000 });
				return; // Success - timeline appeared
			} catch {
				// Timeline didn't appear, retry click
			}
		}

		// Final attempt with longer timeout
		await this.page.waitForSelector('h3:has-text("Agent Timeline")', { timeout: 15000 });
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
		await card.locator('.action-btn.pause').click();
	}

	async resumeLoop(loopIdSuffix: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await card.locator('.action-btn.resume').click();
	}

	async cancelLoop(loopIdSuffix: string): Promise<void> {
		const card = await this.getLoopCard(loopIdSuffix);
		await card.locator('.action-btn.cancel').click();
	}

	// Collapsible Panel methods
	async expectFeedPanelVisible(): Promise<void> {
		await expect(this.feedPanel).toBeVisible();
	}

	async expectLoopsPanelVisible(): Promise<void> {
		await expect(this.loopsPanel).toBeVisible();
	}

	async expectChatPanelVisible(): Promise<void> {
		await expect(this.chatPanel).toBeVisible();
	}

	async toggleFeedPanel(): Promise<void> {
		await this.feedPanel.locator('.collapse-toggle').click();
	}

	async toggleLoopsPanel(): Promise<void> {
		await this.loopsPanel.locator('.collapse-toggle').click();
	}

	async toggleChatPanel(): Promise<void> {
		await this.chatPanel.locator('.collapse-toggle').click();
	}

	async expectFeedPanelCollapsed(): Promise<void> {
		await expect(this.feedPanel).toHaveClass(/collapsed/);
	}

	async expectLoopsPanelCollapsed(): Promise<void> {
		await expect(this.loopsPanel).toHaveClass(/collapsed/);
	}

	async expectChatPanelCollapsed(): Promise<void> {
		await expect(this.chatPanel).toHaveClass(/collapsed/);
	}

	async expectFeedPanelExpanded(): Promise<void> {
		await expect(this.feedPanel).not.toHaveClass(/collapsed/);
	}

	async expectLoopsPanelExpanded(): Promise<void> {
		await expect(this.loopsPanel).not.toHaveClass(/collapsed/);
	}

	async expectChatPanelExpanded(): Promise<void> {
		await expect(this.chatPanel).not.toHaveClass(/collapsed/);
	}
}
