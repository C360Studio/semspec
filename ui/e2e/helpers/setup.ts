import { test as base, expect } from '@playwright/test';
import { ChatPage } from '../pages/ChatPage';
import { SidebarPage } from '../pages/SidebarPage';
import { EntitiesPage, EntityDetailPage } from '../pages/EntitiesPage';
import { LoopPanelPage } from '../pages/LoopPanelPage';

/**
 * Extended test fixtures for semspec-ui E2E tests.
 *
 * Provides pre-configured page objects for common UI components.
 */
export const test = base.extend<{
	chatPage: ChatPage;
	sidebarPage: SidebarPage;
	entitiesPage: EntitiesPage;
	entityDetailPage: EntityDetailPage;
	loopPanelPage: LoopPanelPage;
}>({
	chatPage: async ({ page }, use) => {
		const chatPage = new ChatPage(page);
		await use(chatPage);
	},
	sidebarPage: async ({ page }, use) => {
		const sidebarPage = new SidebarPage(page);
		await use(sidebarPage);
	},
	entitiesPage: async ({ page }, use) => {
		const entitiesPage = new EntitiesPage(page);
		await use(entitiesPage);
	},
	entityDetailPage: async ({ page }, use) => {
		const entityDetailPage = new EntityDetailPage(page);
		await use(entityDetailPage);
	},
	loopPanelPage: async ({ page }, use) => {
		const loopPanelPage = new LoopPanelPage(page);
		await use(loopPanelPage);
	},
});

export { expect };

/**
 * Wait for the backend to be healthy.
 *
 * Use this before tests that need the full backend stack.
 */
export async function waitForBackendHealth(baseURL: string, timeout = 30000): Promise<void> {
	const start = Date.now();
	const healthURL = `${baseURL}/agentic-dispatch/health`;

	while (Date.now() - start < timeout) {
		try {
			const response = await fetch(healthURL);
			if (response.ok) {
				return;
			}
		} catch {
			// Backend not ready yet
		}
		await new Promise(resolve => setTimeout(resolve, 500));
	}

	throw new Error(`Backend health check timed out after ${timeout}ms`);
}

/**
 * Wait for the activity stream to connect.
 *
 * Checks that the SSE connection is established.
 */
export async function waitForActivityConnection(
	page: import('@playwright/test').Page,
	timeout = 10000
): Promise<void> {
	// Wait for the activity store to indicate connected status
	await page.waitForFunction(
		() => {
			// Check if the system status shows healthy (indicates connection)
			const statusIndicator = document.querySelector('.status-indicator.healthy');
			return statusIndicator !== null;
		},
		{ timeout }
	);
}

/**
 * Test data generators for creating realistic test scenarios.
 */
export const testData = {
	/**
	 * Generate a simple chat message.
	 */
	simpleMessage(): string {
		return 'Hello, this is a test message';
	},

	/**
	 * Generate a command-style message.
	 */
	commandMessage(command: string): string {
		return `/${command}`;
	},

	/**
	 * Generate a status command.
	 */
	statusCommand(): string {
		return '/status';
	},

	/**
	 * Generate a help command.
	 */
	helpCommand(): string {
		return '/help';
	},

	/**
	 * Generate a propose command with description.
	 */
	proposeCommand(description: string): string {
		return `/propose ${description}`;
	},
};

/**
 * Retry a function until it succeeds or times out.
 */
export async function retry<T>(
	fn: () => Promise<T>,
	options: { timeout?: number; interval?: number; message?: string } = {}
): Promise<T> {
	const { timeout = 10000, interval = 500, message = 'Retry timed out' } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		try {
			return await fn();
		} catch {
			await new Promise(resolve => setTimeout(resolve, interval));
		}
	}

	throw new Error(message);
}
