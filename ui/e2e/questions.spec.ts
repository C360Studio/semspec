import { test, expect, waitForHydration } from './helpers/setup';
import type { Page } from '@playwright/test';

/**
 * Question data for mocking.
 */
interface MockQuestion {
	id: string;
	topic: string;
	status: 'pending' | 'answered' | 'timeout';
	question: string;
	from_agent?: string;
	urgency?: 'low' | 'normal' | 'high' | 'blocking';
	context?: string;
	answer?: string;
	answered_by?: string;
	answerer_type?: string;
	answered_at?: string;
}

/**
 * Set up mocks needed for the layout to load cleanly, then mock the questions
 * API so questionsStore.fetch() returns the provided questions.
 *
 * IMPORTANT: Call this BEFORE page.goto() so routes are registered before
 * the layout's onMount fires and requests begin.
 */
async function setupMocks(page: Page, questions: MockQuestion[] = []) {
	const now = new Date().toISOString();

	const withDefaults = questions.map((q) => ({
		created_at: now,
		from_agent: 'test-agent',
		urgency: 'normal' as const,
		...q,
	}));

	// Questions list endpoint (questionsStore.fetch on connect)
	await page.route('**/workflow-api/questions**', (route) => {
		const url = route.request().url();
		if (url.includes('/stream')) {
			// SSE stream — return heartbeat and keep open
			route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				headers: { 'Cache-Control': 'no-cache', Connection: 'keep-alive' },
				body: 'event: heartbeat\ndata: {}\n\n',
			});
		} else if (route.request().method() === 'POST') {
			// Answer submission
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({}),
			});
		} else {
			// GET list
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ questions: withDefaults, total: withDefaults.length }),
			});
		}
	});

	// Stub layout data endpoints so the page loads without backend
	await page.route('**/workflow-api/plans**', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ plans: [], total: 0 }),
		});
	});

	await page.route('**/agentic-dispatch/loops**', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ loops: [] }),
		});
	});

	await page.route('**/agentic-dispatch/health**', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ healthy: true }),
		});
	});
}

/**
 * Navigate to the app and expand the chat bar so question messages are visible.
 */
async function openChat(page: Page) {
	await page.goto('/');
	await waitForHydration(page);

	// The BottomChatBar starts collapsed — expand it to reveal the MessageList
	const toggle = page.locator('[data-testid="chat-bar-toggle"]');
	await toggle.click();

	// Wait for the message log to become visible
	await expect(page.locator('[role="log"][aria-label="Chat messages"]')).toBeVisible();
}

test.describe('Inline Question Messages', () => {
	test.describe('Empty state', () => {
		test('shows empty chat state when no questions exist', async ({ page }) => {
			await setupMocks(page, []);
			await openChat(page);

			const log = page.locator('[role="log"][aria-label="Chat messages"]');
			// No question messages rendered
			await expect(log.locator('.question-message')).toHaveCount(0);
			// Empty state prompt is shown
			await expect(log.locator('.empty-state')).toBeVisible();
		});
	});

	test.describe('Question rendering', () => {
		test('renders a pending question in the chat log', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-render', topic: 'api.design', status: 'pending', question: 'Which auth strategy should we use?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message')).toHaveCount(1);
		});

		test('displays question text', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-text', topic: 'db', status: 'pending', question: 'Should we use PostgreSQL or SQLite?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message .question-text')).toContainText(
				'Should we use PostgreSQL or SQLite?'
			);
		});

		test('displays question topic in header', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-topic', topic: 'architecture.db', status: 'pending', question: 'Test?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message .topic')).toContainText('architecture.db');
		});

		test('pending question has .pending CSS class', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-pending', topic: 'test', status: 'pending', question: 'Pending?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message.pending')).toHaveCount(1);
		});

		test('question answered via SSE arrives with .answered class and shows answer text', async ({ page }) => {
			// The SSE stream delivers a question_answered event immediately on connect.
			// This updates the pending question in-place and applies the .answered class.
			const now = new Date().toISOString();
			const answeredPayload = {
				id: 'q-sse-answered',
				topic: 'test',
				status: 'answered',
				question: 'What is 2+2?',
				answer: 'Four.',
				answered_by: 'human',
				from_agent: 'test-agent',
				urgency: 'normal',
				created_at: now,
			};
			const sseBody = [
				'event: heartbeat\ndata: {}\n\n',
				`event: question_answered\ndata: ${JSON.stringify(answeredPayload)}\n\n`,
			].join('');

			await setupMocks(page, [
				{ id: 'q-sse-answered', topic: 'test', status: 'pending', question: 'What is 2+2?' },
			]);

			// Override the stream route AFTER setupMocks so this registration wins
			await page.route('**/workflow-api/questions/stream**', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'text/event-stream',
					headers: { 'Cache-Control': 'no-cache', Connection: 'keep-alive' },
					body: sseBody,
				});
			});

			await openChat(page);

			await expect(page.locator('.question-message.answered')).toHaveCount(1);
			await expect(page.locator('.question-message .answer-text')).toContainText('Four.');
		});

		test('blocking question has .blocking CSS class', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-block', topic: 'critical', status: 'pending', question: 'URGENT!', urgency: 'blocking' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message.blocking')).toHaveCount(1);
		});

		test('blocking question shows BLOCKING label in header', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-label', topic: 'critical', status: 'pending', question: 'Blocking!', urgency: 'blocking' },
			]);
			await openChat(page);

			const header = page.locator('.question-message .question-header');
			await expect(header).toContainText('BLOCKING');
		});

		test('normal question shows QUESTION label in header', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-normal-label', topic: 'test', status: 'pending', question: 'Normal?', urgency: 'normal' },
			]);
			await openChat(page);

			const header = page.locator('.question-message .question-header');
			await expect(header).toContainText('QUESTION');
		});

		test('renders multiple questions as separate message cards', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-multi1', topic: 'api', status: 'pending', question: 'Question one?' },
				{ id: 'q-multi2', topic: 'db', status: 'pending', question: 'Question two?' },
				{ id: 'q-multi3', topic: 'arch', status: 'pending', question: 'Question three?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message')).toHaveCount(3);
		});

		test('question timed out via SSE arrives with .timeout CSS class', async ({ page }) => {
			// The SSE stream delivers a question_timeout event immediately on connect.
			// This updates the pending question in-place and applies the .timeout class.
			const now = new Date().toISOString();
			const timeoutPayload = {
				id: 'q-sse-timeout',
				topic: 'test',
				status: 'timeout',
				question: 'Expired?',
				from_agent: 'test-agent',
				urgency: 'normal',
				created_at: now,
			};
			const sseBody = [
				'event: heartbeat\ndata: {}\n\n',
				`event: question_timeout\ndata: ${JSON.stringify(timeoutPayload)}\n\n`,
			].join('');

			await setupMocks(page, [
				{ id: 'q-sse-timeout', topic: 'test', status: 'pending', question: 'Expired?' },
			]);

			// Override the stream route AFTER setupMocks so this registration wins
			await page.route('**/workflow-api/questions/stream**', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'text/event-stream',
					headers: { 'Cache-Control': 'no-cache', Connection: 'keep-alive' },
					body: sseBody,
				});
			});

			await openChat(page);

			await expect(page.locator('.question-message.timeout')).toHaveCount(1);
		});
	});

	test.describe('Reply form', () => {
		test('pending question shows Reply button', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-reply-btn', topic: 'test', status: 'pending', question: 'Can you answer me?' },
			]);
			await openChat(page);

			await expect(page.locator('.question-message .action-btn.reply')).toBeVisible();
		});

		test('answered question does not show Reply button', async ({ page }) => {
			await setupMocks(page, [
				{
					id: 'q-answered-no-reply',
					topic: 'test',
					status: 'answered',
					question: 'Already answered.',
					answer: 'Yes.',
				},
			]);
			await openChat(page);

			await expect(page.locator('.question-message .action-btn.reply')).not.toBeVisible();
		});

		test('clicking Reply reveals the reply form', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-form-open', topic: 'test', status: 'pending', question: 'Open form?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();

			await expect(page.locator('.question-message .reply-form')).toBeVisible();
		});

		test('reply form has textarea and submit button', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-form-fields', topic: 'test', status: 'pending', question: 'Form fields?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();

			const form = page.locator('.question-message .reply-form');
			await expect(form.locator('textarea')).toBeVisible();
			await expect(form.locator('.btn-submit')).toBeVisible();
		});

		test('submit button is disabled when textarea is empty', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-submit-disabled', topic: 'test', status: 'pending', question: 'Disabled?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();

			await expect(page.locator('.question-message .btn-submit')).toBeDisabled();
		});

		test('submit button enables after typing an answer', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-submit-enable', topic: 'test', status: 'pending', question: 'Enable?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();
			await page.locator('.question-message .reply-form textarea').fill('My answer');

			await expect(page.locator('.question-message .btn-submit')).not.toBeDisabled();
		});

		test('cancel button hides the reply form', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-cancel', topic: 'test', status: 'pending', question: 'Cancel?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();
			await expect(page.locator('.question-message .reply-form')).toBeVisible();

			await page.locator('.question-message .btn-cancel').click();
			await expect(page.locator('.question-message .reply-form')).not.toBeVisible();

			// Reply button returns
			await expect(page.locator('.question-message .action-btn.reply')).toBeVisible();
		});

		test('submitting an answer calls the answer API endpoint', async ({ page }) => {
			const answerRequests: string[] = [];

			await setupMocks(page, [
				{ id: 'q-submit-api', topic: 'test', status: 'pending', question: 'Submit to API?' },
			]);

			// Intercept to capture the request body
			await page.route('**/workflow-api/questions/q-submit-api/answer', (route) => {
				const body = route.request().postData();
				if (body) answerRequests.push(body);
				route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
			});

			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();
			await page.locator('.question-message .reply-form textarea').fill('The answer is 42');
			await page.locator('.question-message .btn-submit').click();

			// Wait for the request to be captured
			await expect.poll(() => answerRequests.length).toBeGreaterThan(0);
			expect(answerRequests[0]).toContain('The answer is 42');
		});

		test('reply form hides after successful answer submission', async ({ page }) => {
			await setupMocks(page, [
				{ id: 'q-after-submit', topic: 'test', status: 'pending', question: 'Hide after submit?' },
			]);
			await openChat(page);

			await page.locator('.question-message .action-btn.reply').click();
			await page.locator('.question-message .reply-form textarea').fill('Done');
			await page.locator('.question-message .btn-submit').click();

			// Form should close after submission completes
			await expect(page.locator('.question-message .reply-form')).not.toBeVisible();
		});
	});
});
