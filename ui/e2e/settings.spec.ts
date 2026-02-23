import { test, expect, waitForHydration } from './helpers/setup';

test.describe('Settings Page', () => {
	test.beforeEach(async ({ page }) => {
		// Clear localStorage before each test
		await page.goto('/');
		await waitForHydration(page);
		await page.evaluate(() => localStorage.clear());
	});

	test.describe('Navigation', () => {
		test('settings link in sidebar navigates to settings page', async ({ page, sidebarPage }) => {
			await sidebarPage.expectVisible();
			await page.click('a[href="/settings"]');
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
			await expect(themeSelect.locator('option[value="system"]')).toBeVisible();
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

			const reducedMotion = page.locator('#reduced-motion');
			await expect(reducedMotion).not.toBeChecked();
		});

		test('toggling reduced motion updates html class', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Enable reduced motion
			await page.locator('#reduced-motion').check();
			await expect(page.locator('html')).toHaveClass(/reduced-motion/);

			// Disable reduced motion
			await page.locator('#reduced-motion').uncheck();
			await expect(page.locator('html')).not.toHaveClass(/reduced-motion/);
		});

		test('reduced motion persists across page reload', async ({ page }) => {
			await page.goto('/settings');
			await waitForHydration(page);

			// Enable reduced motion
			await page.locator('#reduced-motion').check();

			// Reload the page
			await page.reload();
			await waitForHydration(page);

			// Should still be enabled
			await expect(page.locator('#reduced-motion')).toBeChecked();
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
			await page.locator('#reduced-motion').check();

			// Verify changes
			await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

			// Clear all data
			await page.click('button:has-text("Clear All Cached Data")');
			await page.click('button:has-text("Yes, Clear Everything")');

			// Settings should be reset to defaults
			await expect(page.locator('#theme-select')).toHaveValue('dark');
			await expect(page.locator('#activity-limit')).toHaveValue('100');
			await expect(page.locator('#reduced-motion')).not.toBeChecked();
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
