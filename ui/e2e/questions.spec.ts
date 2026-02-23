import { test, expect } from './helpers/setup';

/**
 * Question data for mocking.
 */
interface MockQuestion {
	id: string;
	topic: string;
	status: string;
	question: string;
	from_agent?: string;
	urgency?: string;
	answer?: string;
	answered_by?: string;
	answerer_type?: string;
	answered_at?: string;
}

/**
 * Helper to set up question API mocks.
 * Should be called AFTER page is loaded, then reload.
 */
async function setupQuestionMocks(page: import('@playwright/test').Page, questions: MockQuestion[] = []) {
	// Mock questions list endpoint
	await page.route(/\/questions(\?.*)?$/, (route) => {
		const questionsWithDefaults = questions.map((q) => ({
			from_agent: 'test-agent',
			urgency: 'normal',
			created_at: new Date().toISOString(),
			...q
		}));

		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(questionsWithDefaults)
		});
	});

	// Mock answer endpoint
	await page.route(/\/questions\/[^/]+\/answer$/, (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({})
		});
	});

	// Mock SSE stream - immediate heartbeat then close
	await page.route(/\/questions\/stream$/, (route) => {
		route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			headers: {
				'Cache-Control': 'no-cache',
				Connection: 'keep-alive'
			},
			body: 'event: heartbeat\ndata: {}\n\n'
		});
	});
}

test.describe('Question Management', () => {
	/**
	 * Tests for the QuestionQueue component on the Activity page.
	 *
	 * The QuestionQueue shows pending questions from agents that need human answers.
	 * It only renders when there are pending questions.
	 */

	test.describe('Activity Page Layout', () => {
		test('shows loops section on Activity page', async ({ page }) => {
			await page.goto('/activity');
			const loopsPanel = page.locator('[data-panel-id="activity-loops"]');
			await expect(loopsPanel).toBeVisible();
		});

		test('shows questions section when pending questions exist', async ({ page }) => {
			// Questions section only renders when there are pending questions
			await setupQuestionMocks(page, [
				{ id: 'q-layout', topic: 'test', status: 'pending', question: 'Test?' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const questionsSection = page.locator('.questions-section');
			await expect(questionsSection).toBeVisible();
		});
	});

	test.describe('Question Queue - Empty State', () => {
		test('does not show queue when no pending questions', async ({ page }) => {
			// QuestionQueue component only renders when pendingQuestions.length > 0
			await page.goto('/activity');
			const questionQueue = page.locator('.question-queue');
			// Queue should not be visible when empty
			await expect(questionQueue).not.toBeVisible();
		});
	});

	test.describe('Question Queue - With Questions', () => {
		test('shows queue header with question count', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-test1', topic: 'api.test', status: 'pending', question: 'Test question 1?' },
				{ id: 'q-test2', topic: 'arch.test', status: 'pending', question: 'Test question 2?' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500); // Wait for questions to load

			const queueHeader = page.locator('.queue-header');
			await expect(queueHeader).toBeVisible();

			const queueCount = page.locator('.queue-count');
			await expect(queueCount).toHaveText('2');
		});

		test('shows question items in list', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-item1', topic: 'test.topic', status: 'pending', question: 'What is the answer?' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const questionItems = page.locator('.question-item');
			await expect(questionItems).toHaveCount(1);
		});

		test('displays question text correctly', async ({ page }) => {
			await setupQuestionMocks(page, [
				{
					id: 'q-details',
					topic: 'architecture.db',
					status: 'pending',
					question: 'Should we use PostgreSQL or SQLite?',
					from_agent: 'planner-agent'
				}
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const questionText = page.locator('.question-text');
			await expect(questionText).toContainText('Should we use PostgreSQL or SQLite?');
		});

		test('displays question topic', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-topic', topic: 'api.endpoints', status: 'pending', question: 'Test?' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const questionTopic = page.locator('.question-topic');
			await expect(questionTopic).toContainText('api.endpoints');
		});

		test('displays agent name', async ({ page }) => {
			await setupQuestionMocks(page, [
				{
					id: 'q-agent',
					topic: 'test',
					status: 'pending',
					question: 'Test?',
					from_agent: 'context-builder'
				}
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const questionFrom = page.locator('.question-from');
			await expect(questionFrom).toContainText('context-builder');
		});
	});

	test.describe('Urgency Indicators', () => {
		test('shows blocking badge when blocking questions exist', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-blocking', topic: 'critical', status: 'pending', question: 'Urgent!', urgency: 'blocking' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const blockingBadge = page.locator('.blocking-badge');
			await expect(blockingBadge).toBeVisible();
			await expect(blockingBadge).toContainText('blocking');
		});

		test('shows urgency tag on blocking question', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-block', topic: 'test', status: 'pending', question: 'Blocking!', urgency: 'blocking' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const urgencyTag = page.locator('.urgency-tag.blocking');
			await expect(urgencyTag).toBeVisible();
		});

		test('shows urgency tag on high priority question', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-high', topic: 'test', status: 'pending', question: 'High priority!', urgency: 'high' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const urgencyTag = page.locator('.urgency-tag.high');
			await expect(urgencyTag).toBeVisible();
		});
	});

	test.describe('Answer Form', () => {
		test('shows answer button on question', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-btn', topic: 'test', status: 'pending', question: 'Answer me!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const answerBtn = page.locator('.answer-btn');
			await expect(answerBtn).toBeVisible();
		});

		test('opens chat drawer when clicking answer button', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-form', topic: 'test', status: 'pending', question: 'Open form!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const answerBtn = page.locator('.answer-btn');
			await answerBtn.click();

			// Chat drawer should open with question context
			const chatDrawer = page.locator('.chat-drawer');
			await expect(chatDrawer).toBeVisible();

			// Drawer title should reference the question
			const drawerTitle = page.locator('.drawer-title');
			await expect(drawerTitle).toContainText('Question');
		});

		test('can close chat drawer with escape', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-cancel', topic: 'test', status: 'pending', question: 'Cancel me!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			// Open drawer
			await page.locator('.answer-btn').click();
			await expect(page.locator('.chat-drawer')).toBeVisible();

			// Close with escape
			await page.keyboard.press('Escape');
			await expect(page.locator('.chat-drawer')).not.toBeVisible();

			// Answer button should still be visible
			await expect(page.locator('.answer-btn')).toBeVisible();
		});

		test('chat drawer has send button disabled when input is empty', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-disabled', topic: 'test', status: 'pending', question: 'Disabled submit!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();
			await expect(page.locator('.chat-drawer')).toBeVisible();

			const sendBtn = page.locator('.chat-drawer button[aria-label="Send message"]');
			await expect(sendBtn).toBeDisabled();
		});

		test('chat drawer has send button enabled when input has content', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-enabled', topic: 'test', status: 'pending', question: 'Enable submit!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();
			await expect(page.locator('.chat-drawer')).toBeVisible();

			await page.locator('.chat-drawer textarea').fill('My answer');

			const sendBtn = page.locator('.chat-drawer button[aria-label="Send message"]');
			await expect(sendBtn).not.toBeDisabled();
		});

		test('can send message from chat drawer', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-submit', topic: 'test', status: 'pending', question: 'Submit answer!' }
			]);

			// Mock the message send endpoint
			await page.route('**/agentic-dispatch/message', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						type: 'chat_response',
						content: 'Answer received',
						timestamp: new Date().toISOString()
					})
				});
			});

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();
			await expect(page.locator('.chat-drawer')).toBeVisible();

			await page.locator('.chat-drawer textarea').fill('The answer is 42');
			await page.locator('.chat-drawer button[aria-label="Send message"]').click();

			// Wait for message to appear
			await page.waitForTimeout(500);

			// Message list should show the user's message
			const messageList = page.locator('.chat-drawer [role="log"]');
			await expect(messageList).toContainText('The answer is 42');
		});
	});
});
