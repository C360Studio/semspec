import { test, expect, waitForHydration } from './helpers/setup';

test.describe('Settings Page', () => {
	test.beforeEach(async ({ page }) => {
		// Clear localStorage before each test
		// Board is the homepage, use explicit route for initial load
		await page.goto('/board');
		await waitForHydration(page);
		await page.evaluate(() => localStorage.clear());
	});

	test.describe('Navigation', () => {
		test('settings page is accessible via URL', async ({ page }) => {
			// Navigate directly to settings page (avoiding wizard on root page)
			await page.goto('/settings');
			await waitForHydration(page);
			await expect(page).toHaveURL('/settings');
			await expect(page.locator('h1')).toHaveText('Settings');
		});

		test('settings page shows all sections', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Check all sections are present
			await expect(page.locator('h2:has-text("Appearance")')).toBeVisible();
			await expect(page.locator('h2:has-text("Data & Storage")')).toBeVisible();
			await expect(page.locator('h2:has-text("About")')).toBeVisible();
		});
	});

	test.describe('Theme Toggle', () => {
		test('defaults to dark theme', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			const themeSelect = page.locator('#theme-select');
			await expect(themeSelect).toHaveValue('dark');
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
		});

		test('changing theme updates UI immediately', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			const themeSelect = page.locator('#theme-select');

			// Change to light theme
			await themeSelect.selectOption('light');
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

			// Change back to dark theme
			await themeSelect.selectOption('dark');
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
		});

		test('theme persists across page reload', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Change to light theme
			await page.locator('#theme-select').selectOption('light');
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

			// Reload the page
			await page.reload();
			await waitForHydration(page);

			// Theme should still be light
			await expect(page.locator('#theme-select')).toHaveValue('light');
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
		});

		test('system theme option is available', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			const themeSelect = page.locator('#theme-select');
			// Options aren't "visible" until dropdown is opened - check for existence instead
			await expect(themeSelect.locator('option[value="system"]')).toBeAttached();
		});
	});

	test.describe('Activity Limit', () => {
		test('defaults to 100 events', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			const activityLimit = page.locator('#activity-limit');
			await expect(activityLimit).toHaveValue('100');
		});

		test('activity limit persists across page reload', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Change to 250 events
			await page.locator('#activity-limit').selectOption('250');

			// Reload the page
			await page.reload();
			await waitForHydration(page);

			// Should still be 250
			await expect(page.locator('#activity-limit')).toHaveValue('250');
		});
	});

	test.describe('Reduced Motion', () => {
		test('defaults to off', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Use role-based selector since native checkbox may be visually hidden
			const reducedMotion = page.getByRole('checkbox', { name: 'Reduced Motion' });
			await expect(reducedMotion).not.toBeChecked();
		});

		test('toggling reduced motion updates html class', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click the toggle slider instead of the hidden checkbox
			const toggle = page.locator('.toggle').filter({ has: page.locator('#reduced-motion') });
			await toggle.click();
			await expect(page.locator('html')).toHaveClass(/reduced-motion/);

			// Click again to disable
			await toggle.click();
			await expect(page.locator('html')).not.toHaveClass(/reduced-motion/);
		});

		test('reduced motion persists across page reload', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click the toggle slider
			const toggle = page.locator('.toggle').filter({ has: page.locator('#reduced-motion') });
			await toggle.click();

			// Reload the page
			await page.reload();
			await waitForHydration(page);

			// Should still be enabled
			const reducedMotion = page.getByRole('checkbox', { name: 'Reduced Motion' });
			await expect(reducedMotion).toBeChecked();
			await expect(page.locator('html')).toHaveClass(/reduced-motion/);
		});
	});

	test.describe('Clear Data Actions', () => {
		test('clear activity shows confirmation', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click clear activity
			await page.click('button:has-text("Clear Activity")');

			// Confirmation should appear
			await expect(page.locator('text=Clear activity?')).toBeVisible();
			await expect(page.locator('button:has-text("Yes")')).toBeVisible();
			await expect(page.locator('button:has-text("No")')).toBeVisible();
		});

		test('clear activity can be cancelled', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click clear activity, then cancel
			await page.click('button:has-text("Clear Activity")');
			await page.click('button:has-text("No")');

			// Confirmation should disappear
			await expect(page.locator('text=Clear activity?')).not.toBeVisible();
			await expect(page.locator('button:has-text("Clear Activity")')).toBeVisible();
		});

		test('clear messages shows confirmation', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click clear messages
			await page.click('button:has-text("Clear Messages")');

			// Confirmation should appear
			await expect(page.locator('text=Clear messages?')).toBeVisible();
		});

		test('clear all data shows warning confirmation', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Click clear all
			await page.click('button:has-text("Clear All Cached Data")');

			// Warning confirmation should appear
			await expect(page.locator('text=This will reset all settings')).toBeVisible();
			await expect(page.locator('button:has-text("Yes, Clear Everything")')).toBeVisible();
			await expect(page.locator('button:has-text("Cancel")')).toBeVisible();
		});

		test('clear all data resets settings to defaults', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Change some settings first
			await page.locator('#theme-select').selectOption('light');
			await page.locator('#activity-limit').selectOption('500');

			// Click the toggle to enable reduced motion
			const toggle = page.locator('.toggle').filter({ has: page.locator('#reduced-motion') });
			await toggle.click();

			// Verify changes
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

			// Clear all data
			await page.click('button:has-text("Clear All Cached Data")');
			await page.click('button:has-text("Yes, Clear Everything")');

			// Settings should be reset to defaults
			await expect(page.locator('#theme-select')).toHaveValue('dark');
			await expect(page.locator('#activity-limit')).toHaveValue('100');
			const reducedMotion = page.getByRole('checkbox', { name: 'Reduced Motion' });
			await expect(reducedMotion).not.toBeChecked();
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
		});
	});

	test.describe('About Section', () => {
		test('shows version number', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			await expect(page.locator('text=Version')).toBeVisible();
			await expect(page.locator('text=0.1.0')).toBeVisible();
		});

		test('shows API URL', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			await expect(page.locator('text=API')).toBeVisible();
			// Default API URL should be shown
			await expect(page.locator('.about-value.mono')).toBeVisible();
		});
	});
});
