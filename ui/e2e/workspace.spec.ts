import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 workspace', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/workspace');
		await waitForHydration(page);
	});

	test('renders workspace page with heading', async ({ page }) => {
		await expect(page.getByRole('heading', { name: /Workspace/i })).toBeVisible();
	});

	test('shows empty state or task list', async ({ page }) => {
		const empty = page.getByTestId('workspace-empty');
		const taskList = page.getByTestId('workspace-task-list');

		const hasEmpty = await empty.isVisible().catch(() => false);
		const hasTasks = await taskList.isVisible().catch(() => false);
		expect(hasEmpty || hasTasks).toBe(true);
	});

	test('empty state shows helpful message', async ({ page }) => {
		const empty = page.getByTestId('workspace-empty');
		const isEmpty = await empty.isVisible().catch(() => false);

		if (isEmpty) {
			await expect(page.getByText(/No Active Worktrees/i)).toBeVisible();
		}
	});

	test('shows refresh button', async ({ page }) => {
		const refreshBtn = page.getByRole('button', { name: /Refresh/i });
		await expect(refreshBtn).toBeVisible();
		await refreshBtn.click();
		// Should stay on workspace page after refresh
		await expect(page.getByRole('heading', { name: /Workspace/i })).toBeVisible();
	});

	test('task cards are clickable when tasks exist', async ({ page }) => {
		const taskList = page.getByTestId('workspace-task-list');
		const hasTasks = await taskList.isVisible().catch(() => false);

		if (hasTasks) {
			// First task card should be clickable
			const firstTask = page.locator('[data-testid^="workspace-task-"]').first();
			await expect(firstTask).toBeVisible();
		}
	});

});
