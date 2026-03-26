import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 settings', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/settings');
		await waitForHydration(page);
	});

	test('renders settings page with all sections', async ({ page }) => {
		await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'Appearance' })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'Data & Storage' })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'About' })).toBeVisible();
	});

	test('shows version and API info', async ({ page }) => {
		await expect(page.getByText('0.1.0')).toBeVisible();
		// "API" label is in the About section — scope to center panel
		await expect(page.getByTestId('panel-center').getByText('API')).toBeVisible();
	});

	test('theme selector changes value', async ({ page }) => {
		const themeSelect = page.locator('#theme-select');
		await expect(themeSelect).toBeVisible();

		await themeSelect.selectOption('light');
		await expect(themeSelect).toHaveValue('light');

		await themeSelect.selectOption('dark');
		await expect(themeSelect).toHaveValue('dark');

		await themeSelect.selectOption('system');
		await expect(themeSelect).toHaveValue('system');
	});

	test('reduced motion toggle works', async ({ page }) => {
		// The checkbox is opacity: 0 inside a custom .toggle label.
		// Click the visible toggle slider (sibling span) instead.
		const toggleSlider = page.locator('#reduced-motion + .toggle-slider');
		const checkbox = page.locator('#reduced-motion');

		const wasChecked = await checkbox.isChecked();
		await toggleSlider.click();

		if (wasChecked) {
			await expect(checkbox).not.toBeChecked();
		} else {
			await expect(checkbox).toBeChecked();
		}

		// Reset
		await toggleSlider.click();
	});

	test('activity limit selector changes value', async ({ page }) => {
		const limitSelect = page.locator('#activity-limit');
		await expect(limitSelect).toBeVisible();

		await limitSelect.selectOption('250');
		await expect(limitSelect).toHaveValue('250');

		// Reset
		await limitSelect.selectOption('100');
	});

	test('clear activity shows confirmation then dismisses', async ({ page }) => {
		const clearBtn = page.getByRole('button', { name: /Clear Activity/i });
		await expect(clearBtn).toBeVisible();
		await clearBtn.click();

		// Confirmation appears
		await expect(page.getByText('Clear activity?')).toBeVisible();
		const noBtn = page.getByRole('button', { name: 'No' }).first();
		await noBtn.click();

		// Confirmation dismissed, original button returns
		await expect(clearBtn).toBeVisible();
	});

	test('clear activity confirmation executes', async ({ page }) => {
		await page.getByRole('button', { name: /Clear Activity/i }).click();
		await expect(page.getByText('Clear activity?')).toBeVisible();

		await page.getByRole('button', { name: 'Yes' }).first().click();

		// Confirmation dismissed, button returns to normal
		await expect(page.getByRole('button', { name: /Clear Activity/i })).toBeVisible();
	});

	test('clear messages shows confirmation then dismisses', async ({ page }) => {
		await page.getByRole('button', { name: /Clear Messages/i }).click();
		await expect(page.getByText('Clear messages?')).toBeVisible();

		await page.getByRole('button', { name: 'No' }).nth(0).click();
		await expect(page.getByRole('button', { name: /Clear Messages/i })).toBeVisible();
	});

	test('clear all data shows warning confirmation', async ({ page }) => {
		await page.getByRole('button', { name: /Clear All Cached Data/i }).click();

		await expect(page.getByText(/reset all settings/i)).toBeVisible();
		await expect(page.getByRole('button', { name: /Yes, Clear Everything/i })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();

		// Cancel
		await page.getByRole('button', { name: 'Cancel' }).click();
		await expect(page.getByRole('button', { name: /Clear All Cached Data/i })).toBeVisible();
	});
});
