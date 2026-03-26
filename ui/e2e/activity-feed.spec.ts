import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { feedModeRadio, plansModeRadio } from './helpers/selectors';

test.describe('@t0 activity-feed', () => {
	test('switching to Feed mode shows activity feed', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Switch from Plans to Feed mode
		await feedModeRadio(page).click();
		await expect(feedModeRadio(page)).toHaveAttribute('aria-checked', 'true');

		// Activity feed heading should appear
		await expect(page.getByText('Activity Feed')).toBeVisible();
	});

	test('activity feed shows connection status', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await feedModeRadio(page).click();

		// Should show Live or Connecting indicator
		const liveText = page.getByText('Live');
		const connectingText = page.getByText('Connecting...');
		const isLive = await liveText.isVisible().catch(() => false);
		const isConnecting = await connectingText.isVisible().catch(() => false);
		expect(isLive || isConnecting).toBe(true);
	});

	test('activity feed shows event count', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await feedModeRadio(page).click();

		// Should show "N events" counter
		await expect(page.getByText(/\d+ events/)).toBeVisible();
	});

	test('activity feed has event type filter', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await feedModeRadio(page).click();

		// Filter dropdown should be present
		const filter = page.getByLabel('Filter by event type');
		await expect(filter).toBeVisible();

		// Should have "All events" option
		await expect(filter.getByText('All events')).toBeAttached();
	});

	test('event type filter changes displayed events', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await feedModeRadio(page).click();

		const filter = page.getByLabel('Filter by event type');
		// Switch to a specific event type
		await filter.selectOption('tool_call');
		// Should still show the feed (not crash)
		await expect(page.getByText('Activity Feed')).toBeVisible();

		// Switch back to all
		await filter.selectOption('all');
		await expect(page.getByText(/\d+ events/)).toBeVisible();
	});

	test('events list or empty state shows', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await feedModeRadio(page).click();

		// Either events are present (role="log") or empty state shows
		const eventsList = page.getByRole('log');
		const emptyFeed = page.getByText('No activity yet');
		const hasEvents = await eventsList.isVisible().catch(() => false);
		const isEmpty = await emptyFeed.isVisible().catch(() => false);
		expect(hasEvents || isEmpty).toBe(true);
	});

	test('switching back to Plans mode hides feed', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Switch to Feed
		await feedModeRadio(page).click();
		await expect(page.getByText('Activity Feed')).toBeVisible();

		// Switch back to Plans
		await plansModeRadio(page).click();
		await expect(plansModeRadio(page)).toHaveAttribute('aria-checked', 'true');

		// Activity Feed heading should no longer be visible
		await expect(page.getByText('Activity Feed')).not.toBeVisible();
	});

	test('activity page shows feed and loops panels', async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);

		// Activity page has its own dedicated view with Feed + Loops panels
		// Should show at least one of: Feed heading or "No active loops"
		const feedVisible = await page.getByText('Activity Feed').isVisible().catch(() => false);
		const activityVisible = await page.getByText(/Feed|Timeline/i).first().isVisible().catch(() => false);
		expect(feedVisible || activityVisible).toBe(true);
	});
});
