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
			const loopsSection = page.locator('.loops-section');
			await expect(loopsSection).toBeVisible();
		});

		test('shows questions section on Activity page', async ({ page }) => {
			await page.goto('/activity');
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

		test('opens answer form when clicking answer button', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-form', topic: 'test', status: 'pending', question: 'Open form!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			const answerBtn = page.locator('.answer-btn');
			await answerBtn.click();

			const answerForm = page.locator('.answer-form');
			await expect(answerForm).toBeVisible();
		});

		test('can cancel answer form', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-cancel', topic: 'test', status: 'pending', question: 'Cancel me!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			// Open form
			await page.locator('.answer-btn').click();
			await expect(page.locator('.answer-form')).toBeVisible();

			// Cancel
			await page.locator('.btn-cancel').click();
			await expect(page.locator('.answer-form')).not.toBeVisible();

			// Answer button should be back
			await expect(page.locator('.answer-btn')).toBeVisible();
		});

		test('submit button is disabled when textarea is empty', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-disabled', topic: 'test', status: 'pending', question: 'Disabled submit!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();

			const submitBtn = page.locator('.btn-submit');
			await expect(submitBtn).toBeDisabled();
		});

		test('submit button is enabled when textarea has content', async ({ page }) => {
			await setupQuestionMocks(page, [
				{ id: 'q-enabled', topic: 'test', status: 'pending', question: 'Enable submit!' }
			]);

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();
			await page.locator('.answer-form textarea').fill('My answer');

			const submitBtn = page.locator('.btn-submit');
			await expect(submitBtn).not.toBeDisabled();
		});

		test('can submit answer', async ({ page }) => {
			let answerSent = false;
			let answerBody: { answer?: string } = {};

			await setupQuestionMocks(page, [
				{ id: 'q-submit', topic: 'test', status: 'pending', question: 'Submit answer!' }
			]);

			// Add route for answer submission
			await page.route(/\/questions\/q-submit\/answer$/, (route) => {
				answerSent = true;
				answerBody = route.request().postDataJSON() as typeof answerBody;
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({})
				});
			});

			await page.goto('/activity');
			await page.waitForTimeout(500);

			await page.locator('.answer-btn').click();
			await page.locator('.answer-form textarea').fill('The answer is 42');
			await page.locator('.btn-submit').click();

			await page.waitForTimeout(500);
			expect(answerSent).toBe(true);
			expect(answerBody.answer).toBe('The answer is 42');
		});
	});
});
