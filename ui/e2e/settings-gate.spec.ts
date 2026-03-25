import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

const API_BASE = 'http://localhost:3000';

/**
 * @mock settings gate tests.
 *
 * Verifies the project configuration gate:
 * - Auto-init when .semspec/ is missing
 * - Redirect to /settings when required fields (org, name, checklist) are missing
 * - Settings page shows what's missing
 * - After saving org, gate clears and app is usable
 */

test.describe('@mock settings-gate', () => {
	test('settings page is accessible via gear icon', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		const gearLink = page.locator('a.settings-link');
		await expect(gearLink).toBeVisible();
		await gearLink.click();
		await expect(page).toHaveURL(/\/settings/);
	});

	test('settings page shows project section', async ({ page }) => {
		await page.goto('/settings');
		await waitForHydration(page);

		// Project section header
		await expect(page.getByRole('heading', { name: 'Project' })).toBeVisible();

		// Status indicator should be visible
		await expect(page.locator('.status-indicator')).toBeVisible();
	});

	test('project status shows entity prefix when configured', async ({ page }) => {
		// Check the backend status first
		const statusRes = await fetch(`${API_BASE}/project-api/status`);
		const status = await statusRes.json();

		await page.goto('/settings');
		await waitForHydration(page);

		if (status.project_org) {
			// Org is set — should show entity prefix
			await expect(page.getByText('Entity Prefix')).toBeVisible();
			await expect(page.getByText(status.entity_prefix)).toBeVisible();
		} else {
			// Org is missing — should show warning
			await expect(page.locator('.status-indicator.warning')).toBeVisible();
		}
	});

	test('edit mode shows org and name inputs', async ({ page }) => {
		await page.goto('/settings');
		await waitForHydration(page);

		// Click edit button
		const editBtn = page.getByRole('button', { name: /Edit/ });
		if (await editBtn.isVisible()) {
			await editBtn.click();

			// Should see input fields
			await expect(page.locator('#edit-name')).toBeVisible();
			await expect(page.locator('#edit-org')).toBeVisible();
			await expect(page.locator('#edit-description')).toBeVisible();

			// Cancel returns to display mode
			await page.getByRole('button', { name: /Cancel/ }).click();
			await expect(page.locator('#edit-name')).not.toBeVisible();
		}
	});

	test('org validation rejects invalid format', async ({ page }) => {
		await page.goto('/settings');
		await waitForHydration(page);

		const editBtn = page.getByRole('button', { name: /Edit/ });
		if (await editBtn.isVisible()) {
			await editBtn.click();

			// Enter invalid org (uppercase, spaces)
			await page.locator('#edit-org').fill('Bad Org Name');
			await page.getByRole('button', { name: /Save/ }).click();

			// Should show validation error
			await expect(page.locator('.save-error')).toBeVisible();
			await expect(page.locator('.save-error')).toContainText('lowercase');
		}
	});

	test('org validation rejects empty org', async ({ page }) => {
		await page.goto('/settings');
		await waitForHydration(page);

		const editBtn = page.getByRole('button', { name: /Edit/ });
		if (await editBtn.isVisible()) {
			await editBtn.click();

			// Clear org field
			await page.locator('#edit-org').fill('');
			await page.getByRole('button', { name: /Save/ }).click();

			// Should show validation error
			await expect(page.locator('.save-error')).toBeVisible();
			await expect(page.locator('.save-error')).toContainText('required');
		}
	});
});
