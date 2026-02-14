import { test as base, expect } from '@playwright/test';
import { ChatPage } from '../pages/ChatPage';
import { SidebarPage } from '../pages/SidebarPage';
import { EntitiesPage, EntityDetailPage } from '../pages/EntitiesPage';
import { LoopPanelPage } from '../pages/LoopPanelPage';
import { QuestionPanelPage } from '../pages/QuestionPanelPage';
import { PlanDetailPage } from '../pages/PlanDetailPage';
import { ActivityPage } from '../pages/ActivityPage';
import { LoopContextPage } from '../pages/LoopContextPage';

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
	questionPanelPage: QuestionPanelPage;
	planDetailPage: PlanDetailPage;
	activityPage: ActivityPage;
	loopContextPage: LoopContextPage;
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
	questionPanelPage: async ({ page }, use) => {
		const questionPanelPage = new QuestionPanelPage(page);
		await use(questionPanelPage);
	},
	planDetailPage: async ({ page }, use) => {
		const planDetailPage = new PlanDetailPage(page);
		await use(planDetailPage);
	},
	activityPage: async ({ page }, use) => {
		const activityPage = new ActivityPage(page);
		await use(activityPage);
	},
	loopContextPage: async ({ page }, use) => {
		const loopContextPage = new LoopContextPage(page);
		await use(loopContextPage);
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

	/**
	 * Generate an ask command.
	 */
	askCommand(topic: string, question: string): string {
		return `/ask ${topic} "${question}"`;
	},

	/**
	 * Generate a questions command.
	 */
	questionsCommand(filter?: string): string {
		return filter ? `/questions ${filter}` : '/questions';
	},

	/**
	 * Generate an answer command.
	 */
	answerCommand(questionId: string, response: string): string {
		return `/answer ${questionId} "${response}"`;
	},

	/**
	 * Generate a design command with workflow slug.
	 */
	designCommand(slug: string): string {
		return `/design ${slug}`;
	},

	/**
	 * Generate a spec command with workflow slug.
	 */
	specCommand(slug: string): string {
		return `/spec ${slug}`;
	},

	/**
	 * Generate a tasks command with workflow slug.
	 */
	tasksCommand(slug: string): string {
		return `/tasks ${slug}`;
	},

	/**
	 * Generate a mock workflow loop.
	 */
	mockWorkflowLoop(overrides: Partial<MockWorkflowLoop> = {}): MockWorkflowLoop {
		const id = overrides.loop_id || `loop-${Math.random().toString(36).slice(2, 10)}`;
		return {
			loop_id: id,
			task_id: `task-${id}`,
			user_id: 'test-user',
			channel_type: 'http',
			channel_id: 'test-channel',
			state: 'executing',
			iterations: 1,
			max_iterations: 10,
			created_at: new Date().toISOString(),
			...overrides
		};
	},

	/**
	 * Generate a mock question object.
	 */
	mockQuestion(overrides: Partial<MockQuestion> = {}): MockQuestion {
		const id = overrides.id || `q-${Math.random().toString(36).slice(2, 10)}`;
		return {
			id,
			from_agent: 'test-agent',
			topic: 'test.topic',
			question: 'What is the answer to this test question?',
			status: 'pending',
			urgency: 'normal',
			created_at: new Date().toISOString(),
			...overrides,
		};
	},

	/**
	 * Generate a mock answered question.
	 */
	mockAnsweredQuestion(overrides: Partial<MockQuestion> = {}): MockQuestion {
		return this.mockQuestion({
			status: 'answered',
			answer: 'This is the test answer.',
			answered_by: 'test-user',
			answerer_type: 'human',
			answered_at: new Date().toISOString(),
			...overrides,
		});
	},
};

interface MockQuestion {
	id: string;
	from_agent: string;
	topic: string;
	question: string;
	context?: string;
	status: 'pending' | 'answered' | 'timeout';
	urgency: 'low' | 'normal' | 'high' | 'blocking';
	created_at: string;
	deadline?: string;
	answer?: string;
	answered_by?: string;
	answerer_type?: 'agent' | 'team' | 'human';
	answered_at?: string;
	confidence?: 'high' | 'medium' | 'low';
	sources?: string;
}

interface MockWorkflowLoop {
	loop_id: string;
	task_id: string;
	user_id: string;
	channel_type: string;
	channel_id: string;
	state: 'pending' | 'exploring' | 'executing' | 'paused' | 'complete' | 'success' | 'failed' | 'cancelled';
	iterations: number;
	max_iterations: number;
	created_at: string;
	workflow_slug?: string;
	workflow_step?: 'propose' | 'design' | 'spec' | 'tasks';
	role?: string;
	model?: string;
}

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
