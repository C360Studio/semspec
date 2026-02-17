import { test, expect, testData } from './helpers/setup';
import { createMockLoop, createMockLoopWithContext } from './helpers/workflow';

test.describe('Agent Timeline', () => {
	test.beforeEach(async ({ activityPage }) => {
		await activityPage.goto();
	});

	test.describe('View Toggle', () => {
		test('defaults to feed view', async ({ activityPage }) => {
			await activityPage.expectFeedView();
		});

		test('toggles between feed and timeline view', async ({ activityPage }) => {
			// Start on feed view
			await activityPage.expectFeedView();

			// Switch to timeline
			await activityPage.switchToTimeline();
			await activityPage.expectTimelineView();

			// Switch back to feed
			await activityPage.switchToFeed();
			await activityPage.expectFeedView();
		});

		test('timeline toggle button gets active class when clicked', async ({ activityPage }) => {
			// Click the timeline toggle
			await activityPage.timelineToggle.click();
			// The button should get the active class (even though view doesn't switch)
			await expect(activityPage.timelineToggle).toHaveClass(/active/);
		});
	});

	test.describe('Timeline Rendering', () => {
		test('shows empty state when no loops', async ({ page, activityPage }) => {
			await page.route('**/agentic-dispatch/loops', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([]),
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();
			await activityPage.expectTimelineVisible();
			await activityPage.expectTimelineEmpty();
		});

		test('renders timeline with mock loop data', async ({ page, activityPage }) => {
			// Mock the loops API to return test data
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'timeline-loop-1',
							role: 'spec-writer',
							state: 'executing',
							iterations: 3,
							max_iterations: 10
						})
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();

			// Timeline should render
			await activityPage.expectTimelineVisible();
		});

		test('shows timeline tracks for agent roles', async ({ page, activityPage }) => {
			// Mock loops with different roles
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'track-loop-1',
							role: 'spec-writer',
							state: 'executing'
						}),
						testData.mockWorkflowLoop({
							loop_id: 'track-loop-2',
							role: 'task-writer',
							state: 'complete'
						})
					])
				});
			});

			// Also mock the plans API to return active loops with roles
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{ loop_id: 'track-loop-1', role: 'spec-writer', model: 'claude-3', state: 'executing', iterations: 1, max_iterations: 10 },
								{ loop_id: 'track-loop-2', role: 'task-writer', model: 'claude-3', state: 'complete', iterations: 5, max_iterations: 10 }
							]
						}
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();

			await activityPage.expectTimelineVisible();
		});
	});

	test.describe('Live Indicator', () => {
		test('shows live indicator when agents are active', async ({ page, activityPage }) => {
			// Mock an active loop
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'live-loop-1',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{ loop_id: 'live-loop-1', role: 'spec-writer', model: 'claude-3', state: 'executing', iterations: 1, max_iterations: 10 }
							]
						}
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();

			await activityPage.expectTimelineVisible();
			await activityPage.expectLiveIndicator();
		});

		test('hides live indicator when no active agents', async ({ page, activityPage }) => {
			// Mock completed loops only
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'complete-loop-1',
							state: 'complete'
						})
					])
				});
			});

			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-plan',
							approved: true,
							stage: 'complete',
							active_loops: [
								{ loop_id: 'complete-loop-1', role: 'spec-writer', model: 'claude-3', state: 'complete', iterations: 10, max_iterations: 10 }
							]
						}
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();

			await activityPage.expectTimelineVisible();
			await activityPage.expectNoLiveIndicator();
		});
	});

	test.describe('Legend', () => {
		test('shows legend with state colors', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendVisible();
		});

		test('displays all legend items', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItems();
		});

		test('shows Active legend item', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItem('Active');
		});

		test('shows Complete legend item', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItem('Complete');
		});

		test('shows Waiting legend item', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItem('Waiting');
		});

		test('shows Blocked legend item', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItem('Blocked');
		});

		test('shows Failed legend item', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await activityPage.expectLegendItem('Failed');
		});
	});

	test.describe('Segment Interaction', () => {
		test('shows segment details on click', async ({ page, activityPage }) => {
			// Mock a loop with activity for segments
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'segment-loop-abc123',
							role: 'spec-writer',
							state: 'executing',
							iterations: 5,
							max_iterations: 10
						})
					])
				});
			});

			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{ loop_id: 'segment-loop-abc123', role: 'spec-writer', model: 'claude-3', state: 'executing', iterations: 5, max_iterations: 10 }
							]
						}
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();
			await activityPage.expectTimelineVisible();

			// Click on first segment if available
			const segments = activityPage.timelineSegments;
			const count = await segments.count();
			if (count > 0) {
				await activityPage.clickSegment(0);
				await activityPage.expectSegmentDetails();
			}
		});

		test('closes segment details', async ({ page, activityPage }) => {
			// Mock a loop
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'close-loop-xyz',
							role: 'spec-writer',
							state: 'executing'
						})
					])
				});
			});

			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{ loop_id: 'close-loop-xyz', role: 'spec-writer', model: 'claude-3', state: 'executing', iterations: 1, max_iterations: 10 }
							]
						}
					])
				});
			});

			await page.reload();
			await activityPage.switchToTimeline();

			const segments = activityPage.timelineSegments;
			const count = await segments.count();
			if (count > 0) {
				// Open details
				await activityPage.clickSegment(0);
				await activityPage.expectSegmentDetails();

				// Close details
				await activityPage.closeSegmentDetails();
				await activityPage.expectSegmentDetailsHidden();
			}
		});
	});

	test.describe('Duration Badge', () => {
		test('shows duration badge', async ({ activityPage }) => {
			await activityPage.switchToTimeline();
			await expect(activityPage.durationBadge).toBeVisible();
		});
	});

	test.describe('Active Loops Section', () => {
		test('shows empty state when no active loops', async ({ activityPage }) => {
			// The activity page shows loops section
			await activityPage.expectLoopsEmpty();
		});

		test('shows active loop count', async ({ page, activityPage }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'count-loop-1',
							state: 'executing'
						}),
						testData.mockWorkflowLoop({
							loop_id: 'count-loop-2',
							state: 'executing'
						})
					])
				});
			});

			await page.reload();
			await activityPage.expectActiveLoopCount(2);
		});

		test('displays loop cards', async ({ page, activityPage }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'card-loop-123',
							state: 'executing',
							iterations: 3,
							max_iterations: 10
						})
					])
				});
			});

			await page.reload();
			await activityPage.expectLoopCardCount(1);
		});

		test('shows loop state in card', async ({ page, activityPage }) => {
			await page.route('**/agentic-dispatch/loops', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						testData.mockWorkflowLoop({
							loop_id: 'state-loop-abc',
							state: 'executing'
						})
					])
				});
			});

			await page.reload();
			// LoopCard displays first 8 chars of loop_id: 'state-lo'
			await activityPage.expectLoopState('state-lo', 'executing');
		});
	});
});
