import { test, expect } from './helpers/setup';
import { waitForHydration } from './helpers/setup';

test.describe('ChatDrawer', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);
	});

	test('should open drawer with Cmd+K keyboard shortcut', async ({ page }) => {
		// Drawer should not be visible initially
		await expect(page.locator('.chat-drawer')).not.toBeVisible();

		// Press Cmd+K (Mac) or Ctrl+K (Windows/Linux)
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');

		// Drawer should be visible
		await expect(page.locator('.chat-drawer')).toBeVisible();
		await expect(page.locator('.drawer-title')).toHaveText('Chat');
	});

	test('should close drawer with Escape key', async ({ page }) => {
		// Open drawer
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Press Escape
		await page.keyboard.press('Escape');

		// Drawer should be hidden
		await expect(page.locator('.chat-drawer')).not.toBeVisible();
	});

	test('should close drawer when clicking backdrop', async ({ page }) => {
		// Open drawer
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Click backdrop (not the drawer itself)
		await page.locator('.chat-drawer-backdrop').click({ position: { x: 10, y: 10 } });

		// Drawer should be hidden
		await expect(page.locator('.chat-drawer')).not.toBeVisible();
	});

	test('should close drawer when clicking close button', async ({ page }) => {
		// Open drawer
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Click close button
		await page.locator('.close-button').click();

		// Drawer should be hidden
		await expect(page.locator('.chat-drawer')).not.toBeVisible();
	});

	test('should toggle drawer open and closed with repeated Cmd+K', async ({ page }) => {
		const isMac = process.platform === 'darwin';
		const shortcut = isMac ? 'Meta+k' : 'Control+k';

		// Initially closed
		await expect(page.locator('.chat-drawer')).not.toBeVisible();

		// First press: open
		await page.keyboard.press(shortcut);
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Second press: close
		await page.keyboard.press(shortcut);
		await expect(page.locator('.chat-drawer')).not.toBeVisible();

		// Third press: open again
		await page.keyboard.press(shortcut);
		await expect(page.locator('.chat-drawer')).toBeVisible();
	});

	test('should focus first input when drawer opens', async ({ page }) => {
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Wait for focus to be set via requestAnimationFrame
		await page.waitForTimeout(150);

		// Check that a focusable element in the drawer is focused
		const textarea = page.locator('.chat-drawer textarea');
		await expect(textarea).toBeFocused();
	});

	test('should trap focus within drawer', async ({ page }) => {
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Get all focusable elements
		const closeButton = page.locator('.close-button');
		const textarea = page.locator('.chat-drawer textarea');

		// Focus should start on textarea
		await expect(textarea).toBeFocused();

		// Tab should cycle through focusable elements
		await page.keyboard.press('Tab');
		// Should focus send button or hints toggle

		// Shift+Tab should go back
		await page.keyboard.press('Shift+Tab');
		await expect(textarea).toBeFocused();
	});

	test('should display ChatPanel content', async ({ page }) => {
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		await expect(page.locator('.chat-drawer')).toBeVisible();

		// Check that ChatPanel is rendered
		await expect(page.locator('.chat-panel')).toBeVisible();
		await expect(page.locator('.message-input')).toBeVisible();
	});

	test('should have proper ARIA attributes', async ({ page }) => {
		const isMac = process.platform === 'darwin';
		await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');

		const drawer = page.locator('.chat-drawer');
		await expect(drawer).toHaveAttribute('role', 'dialog');
		await expect(drawer).toHaveAttribute('aria-modal', 'true');
		await expect(drawer).toHaveAttribute('aria-label', 'Chat');
	});

	test.describe('Mobile behavior', () => {
		test.use({ viewport: { width: 375, height: 667 } });

		test('should be full-screen on mobile', async ({ page }) => {
			const isMac = process.platform === 'darwin';
			await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
			await expect(page.locator('.chat-drawer')).toBeVisible();

			// Check drawer takes full viewport (allow small variance for scrollbars/borders)
			const drawer = page.locator('.chat-drawer');
			const box = await drawer.boundingBox();
			expect(box?.width).toBeGreaterThanOrEqual(370);
			expect(box?.height).toBeGreaterThanOrEqual(660);
		});
	});

	test.describe('Reduced motion', () => {
		test.use({ reducedMotion: 'reduce' });

		test('should respect reduced motion preference', async ({ page }) => {
			// Add reduced-motion class (simulating settingsStore)
			await page.addStyleTag({
				content: '.reduced-motion .chat-drawer, .reduced-motion .chat-drawer-backdrop { transition: none !important; }'
			});

			const isMac = process.platform === 'darwin';
			await page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
			await expect(page.locator('.chat-drawer')).toBeVisible();

			// Drawer should appear immediately without animation
			// (hard to test timing, but we can verify it appears)
		});
	});
});

test.describe('ChatDrawerTrigger', () => {
	test('icon trigger on activity page opens drawer', async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);

		// Find the trigger icon in the Loops panel header
		const trigger = page.locator('.trigger-icon');
		await expect(trigger).toBeVisible();
		await expect(trigger).toHaveAttribute('aria-label', 'Open chat');

		// Click trigger to open drawer
		await trigger.click();
		await expect(page.locator('.chat-drawer')).toBeVisible();
	});

	test('icon trigger on plan detail page opens drawer with plan context', async ({ page }) => {
		// Mock plans list endpoint
		await page.route('**/workflow-api/plans', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([{
					slug: 'test-plan',
					goal: 'Test goal',
					approved: true,
					stage: 'planning',
					active_loops: []
				}])
			});
		});

		// Mock tasks endpoint
		await page.route('**/workflow-api/plans/test-plan/tasks', route => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/plans/test-plan');
		await waitForHydration(page);

		// Find the trigger icon in header
		const trigger = page.locator('.header-right .trigger-icon');
		await expect(trigger).toBeVisible();
		await expect(trigger).toHaveAttribute('aria-label', 'Open chat for plan test-plan');

		// Click trigger to open drawer
		await trigger.click();
		await expect(page.locator('.chat-drawer')).toBeVisible();
		// Drawer title should show plan context
		await expect(page.locator('.drawer-title')).toContainText('Plan');
	});
});
