import { test, expect, waitForHydration } from './helpers/setup';

/**
 * Landing page (/) — board view with three-panel shell.
 *
 * Verifies the app shell structure: left panel (plans/feed),
 * center panel (board), header (project name, connection status).
 */
test.describe('Landing Page', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
	});

	test.describe('App Shell', () => {
		test('renders three-panel layout', async ({ page }) => {
			await expect(page.locator('[data-testid="three-panel-layout"]')).toBeVisible();
			await expect(page.locator('[data-testid="panel-left"]')).toBeVisible();
			await expect(page.locator('[data-testid="panel-center"]')).toBeVisible();
		});

		test('header shows project name and connection status', async ({ page }) => {
			const header = page.locator('header.header');
			await expect(header).toBeVisible();
			await expect(header.locator('.status-item.connection')).toBeVisible();
		});

		test('no chat bar is visible', async ({ page }) => {
			await expect(page.locator('[data-testid="bottom-chat-bar"]')).not.toBeVisible();
		});
	});

	test.describe('Left Panel', () => {
		test('shows plans/feed mode switcher', async ({ page }) => {
			const switcher = page.getByRole('radiogroup', { name: 'Left panel mode' });
			await expect(switcher).toBeVisible();
			await expect(page.getByRole('radio', { name: 'Plans' })).toBeChecked();
			await expect(page.getByRole('radio', { name: 'Feed' })).not.toBeChecked();
		});

		test('shows plan filter chips', async ({ page }) => {
			const filters = page.getByRole('radiogroup', { name: 'Filter plans' });
			await expect(filters).toBeVisible();
			await expect(page.getByRole('radio', { name: 'All' })).toBeChecked();
			await expect(page.getByRole('radio', { name: 'Active' })).toBeVisible();
			await expect(page.getByRole('radio', { name: 'Drafts' })).toBeVisible();
			await expect(page.getByRole('radio', { name: 'Done' })).toBeVisible();
		});

		test('shows New Plan link', async ({ page }) => {
			const newPlanLink = page.locator('[data-testid="panel-left"]').getByRole('link', { name: 'New Plan' });
			await expect(newPlanLink).toBeVisible();
			await expect(newPlanLink).toHaveAttribute('href', '/plans/new');
		});

		test('shows empty state when no plans exist', async ({ page }) => {
			await expect(page.locator('[data-testid="panel-left"]').getByText('No plans')).toBeVisible();
		});

		test('switches to feed mode', async ({ page }) => {
			await page.getByRole('radio', { name: 'Feed' }).click();
			await expect(page.getByRole('radio', { name: 'Feed' })).toBeChecked();
		});
	});

	test.describe('Center Panel - Board', () => {
		test('shows board header with title', async ({ page }) => {
			await expect(page.getByRole('heading', { name: 'Active Plans', exact: true })).toBeVisible();
		});

		test('shows grid/kanban view toggle', async ({ page }) => {
			const toggle = page.getByRole('radiogroup', { name: 'Board view mode' });
			await expect(toggle).toBeVisible();
			await expect(page.getByRole('radio', { name: 'Grid view' })).toBeChecked();
			await expect(page.getByRole('radio', { name: 'Kanban view' })).toBeVisible();
		});

		test('shows New Plan button in header', async ({ page }) => {
			const btn = page.locator('button.new-plan-btn');
			await expect(btn).toBeVisible();
		});

		test('shows empty state with create link', async ({ page }) => {
			await expect(page.getByRole('heading', { name: 'No active plans' })).toBeVisible();
			const createLink = page.getByRole('link', { name: 'Create Your First Plan' });
			await expect(createLink).toBeVisible();
			await expect(createLink).toHaveAttribute('href', '/plans/new');
		});

		test('New Plan button navigates to /plans/new', async ({ page }) => {
			await page.locator('button.new-plan-btn').click();
			await expect(page).toHaveURL('/plans/new');
		});

		test('Create Your First Plan link navigates to /plans/new', async ({ page }) => {
			await page.getByRole('link', { name: 'Create Your First Plan' }).click();
			await expect(page).toHaveURL('/plans/new');
		});
	});

	test.describe('Panel Controls', () => {
		test('left panel can be collapsed via toggle button', async ({ page }) => {
			const toggleBtn = page.getByRole('button', { name: /Collapse left panel/ });
			await expect(toggleBtn).toBeVisible();
			await toggleBtn.click();
			await expect(page.locator('[data-testid="panel-left"]')).not.toBeVisible();
		});

		test('Cmd+B toggles left panel', async ({ page }) => {
			await expect(page.locator('[data-testid="panel-left"]')).toBeVisible();
			const isMac = process.platform === 'darwin';
			await page.keyboard.press(isMac ? 'Meta+b' : 'Control+b');
			await expect(page.locator('[data-testid="panel-left"]')).not.toBeVisible();
			await page.keyboard.press(isMac ? 'Meta+b' : 'Control+b');
			await expect(page.locator('[data-testid="panel-left"]')).toBeVisible();
		});
	});
});
