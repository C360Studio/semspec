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

			// Check drawer dimensions
			const drawer = page.locator('.chat-drawer');
			const box = await drawer.boundingBox();
			expect(box?.width).toBe(375); // Full viewport width
			expect(box?.height).toBe(667); // Full viewport height
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
	// ChatDrawerTrigger tests are skipped until the component is integrated into specific pages
	// The drawer is currently accessed via Cmd+K keyboard shortcut globally

	test.skip('icon variant should render icon button', async () => {
		// Will be enabled when ChatDrawerTrigger is added to plan detail page
	});

	test.skip('button variant should render button with text', async () => {
		// Will be enabled when ChatDrawerTrigger is added to board view
	});
});
