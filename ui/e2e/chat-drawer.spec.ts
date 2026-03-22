import { test, expect } from './helpers/setup';
import { waitForHydration } from './helpers/setup';

test.describe('BottomChatBar', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);
	});

	test('is visible in collapsed state on page load', async ({ page }) => {
		const bar = page.getByTestId('bottom-chat-bar');
		await expect(bar).toBeVisible();

		// Body is not present while collapsed
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();
	});

	test('expands when toggle button is clicked', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');
		await toggle.click();

		// Body appears after expanding
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
	});

	test('collapses when toggle button is clicked while expanded', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');

		// Expand
		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		// Collapse
		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();
	});

	test('expands via Cmd+K keyboard shortcut', async ({ page }) => {
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();

		await page.keyboard.press('Meta+k');

		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
	});

	test('collapses via Cmd+K when already expanded', async ({ page }) => {
		// Expand first via button click
		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		// Collapse via keyboard shortcut
		await page.keyboard.press('Meta+k');

		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();
	});

	test('toggles open and closed with repeated Cmd+K presses', async ({ page }) => {
		// Initially collapsed
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();

		// First press: expand
		await page.keyboard.press('Meta+k');
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		// Second press: collapse
		await page.keyboard.press('Meta+k');
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();

		// Third press: expand again
		await page.keyboard.press('Meta+k');
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
	});

	test('toggle button has correct aria-expanded when collapsed', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');
		await expect(toggle).toHaveAttribute('aria-expanded', 'false');
	});

	test('toggle button has correct aria-expanded when expanded', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');
		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		await expect(toggle).toHaveAttribute('aria-expanded', 'true');
	});

	test('toggle button aria-label changes with state', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');

		// Collapsed state
		await expect(toggle).toHaveAttribute('aria-label', 'Expand chat');

		// Expanded state
		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
		await expect(toggle).toHaveAttribute('aria-label', 'Collapse chat');
	});

	test('shows "Chat" label in header', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');
		await expect(toggle).toContainText('Chat');
	});

	test('shows keyboard shortcut hint in header', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');
		await expect(toggle).toContainText('Cmd+K');
	});

	test('expanded body contains message input', async ({ page }) => {
		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		// MessageInput renders a textarea with aria-label="Message input"
		const textarea = page.getByTestId('chat-bar-body').getByRole('textbox', { name: 'Message input' });
		await expect(textarea).toBeVisible();
	});

	test('can type a message in the expanded input', async ({ page }) => {
		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		const textarea = page.getByTestId('chat-bar-body').getByRole('textbox', { name: 'Message input' });
		await textarea.click();
		await textarea.pressSequentially('Hello from the bottom bar');

		await expect(textarea).toHaveValue('Hello from the bottom bar');
	});

	test('send button is accessible within expanded body', async ({ page }) => {
		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		const sendButton = page.getByTestId('chat-bar-body').getByRole('button', { name: 'Send message' });
		await expect(sendButton).toBeVisible();
	});

	test('toggle button is keyboard-focusable', async ({ page }) => {
		const toggle = page.getByTestId('chat-bar-toggle');

		await toggle.focus();
		await expect(toggle).toBeFocused();
	});

	test('bar persists across navigation', async ({ page }) => {
		// Verify bar is visible on activity page
		await expect(page.getByTestId('bottom-chat-bar')).toBeVisible();

		// Navigate to plans page
		await page.goto('/plans');
		await waitForHydration(page);

		// Bar should still be present (it lives in the root layout)
		await expect(page.getByTestId('bottom-chat-bar')).toBeVisible();
	});
});

test.describe('BottomChatBar on plan detail page', () => {
	test('bar is present and functional with plan context', async ({ page }) => {
		const plan = {
			id: 'plan-chat-test',
			slug: 'chat-test-plan',
			title: 'Chat Test Plan',
			goal: 'Test the bottom chat bar on plan pages',
			approved: true,
			stage: 'approved',
			project_id: 'semspec.local.project.default',
			created_at: new Date().toISOString(),
			active_loops: []
		};

		await page.route('**/workflow-api/plans', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([plan])
			});
		});

		await page.route('**/workflow-api/plans/chat-test-plan', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan)
			});
		});

		await page.route('**/workflow-api/plans/chat-test-plan/phases', (route) => {
			route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
		});

		await page.route('**/workflow-api/plans/chat-test-plan/tasks', (route) => {
			route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
		});

		await page.goto('/plans/chat-test-plan');
		await waitForHydration(page);

		// Bar is present in collapsed state
		const bar = page.getByTestId('bottom-chat-bar');
		await expect(bar).toBeVisible();

		// Can expand it
		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
	});
});

test.describe('BottomChatBar mobile layout', () => {
	test.use({ viewport: { width: 375, height: 667 } });

	test('bar is visible in collapsed state on mobile', async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);

		await expect(page.getByTestId('bottom-chat-bar')).toBeVisible();
	});

	test('expands on mobile via toggle button', async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);

		await page.getByTestId('chat-bar-toggle').click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();
	});
});

test.describe('BottomChatBar reduced motion', () => {
	test.use({ reducedMotion: 'reduce' });

	test('expands and collapses correctly with reduced motion preference', async ({ page }) => {
		await page.goto('/activity');
		await waitForHydration(page);

		const toggle = page.getByTestId('chat-bar-toggle');

		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).toBeVisible();

		await toggle.click();
		await expect(page.getByTestId('chat-bar-body')).not.toBeAttached();
	});
});
