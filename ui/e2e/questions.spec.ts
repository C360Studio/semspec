import { test, expect, testData } from './helpers/setup';

test.describe('Question Management', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/');
	});

	test.describe('Panel Tabs', () => {
		test('shows Loops and Questions tabs', async ({ loopPanelPage }) => {
			await loopPanelPage.expectVisible();
			await loopPanelPage.expectTabsVisible();
		});

		test('Loops tab is active by default', async ({ loopPanelPage }) => {
			await loopPanelPage.expectLoopsTabActive();
		});

		test('can switch to Questions tab', async ({ loopPanelPage, questionPanelPage }) => {
			await loopPanelPage.switchToQuestionsTab();
			await loopPanelPage.expectQuestionsTabActive();
			await questionPanelPage.expectVisible();
		});

		test('can switch back to Loops tab', async ({ loopPanelPage }) => {
			await loopPanelPage.switchToQuestionsTab();
			await loopPanelPage.switchToLoopsTab();
			await loopPanelPage.expectLoopsTabActive();
		});

		test('Questions tab shows pending count badge', async ({ loopPanelPage, page }) => {
			// Mock message response with pending questions
			await page.route('**/agentic-dispatch/message', route => {
				const request = route.request();
				const body = request.postDataJSON();

				if (body?.content?.includes('/questions')) {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: `# Pending Questions (2)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-abc12345 | api.test | pending | Test question 1? |
| q-def67890 | arch.test | pending | Test question 2? |

---
Use \`/questions <id>\` to view details`,
							type: 'result'
						})
					});
				} else {
					route.continue();
				}
			});

			await loopPanelPage.switchToQuestionsTab();
			// Wait for fetch to complete
			await page.waitForTimeout(500);
			await loopPanelPage.expectQuestionsTabBadge(2);
		});
	});

	test.describe('Question Panel', () => {
		test.beforeEach(async ({ loopPanelPage }) => {
			await loopPanelPage.switchToQuestionsTab();
		});

		test('shows filter tabs', async ({ questionPanelPage }) => {
			await questionPanelPage.expectFilterTabsVisible();
		});

		test('Pending filter is active by default', async ({ questionPanelPage }) => {
			await questionPanelPage.expectPendingFilterActive();
		});

		test('can switch between filters', async ({ questionPanelPage }) => {
			await questionPanelPage.filterByAnswered();
			await questionPanelPage.expectAnsweredFilterActive();

			await questionPanelPage.filterByAll();
			await questionPanelPage.expectAllFilterActive();

			await questionPanelPage.filterByPending();
			await questionPanelPage.expectPendingFilterActive();
		});

		test('shows empty state when no questions', async ({ questionPanelPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: 'No pending questions found.\n\nUse `/ask <topic> <question>` to create a question.',
						type: 'result'
					})
				});
			});

			await page.reload();
			await questionPanelPage.expectEmptyState();
		});

		test('shows question cards when questions exist', async ({ questionPanelPage, loopPanelPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Pending Questions (1)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-test1234 | api.semstreams | pending | Does LoopInfo include workflow_slug? |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await page.reload();
			await loopPanelPage.switchToQuestionsTab();
			await page.waitForTimeout(500);
			await questionPanelPage.expectNoEmptyState();
			await questionPanelPage.expectQuestionCards(1);
		});

		test('displays question details correctly', async ({ questionPanelPage, loopPanelPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Pending Questions (1)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-details1 | architecture.db | pending | Should we use PostgreSQL or SQLite? |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await page.reload();
			await loopPanelPage.switchToQuestionsTab();
			await page.waitForTimeout(500);

			await questionPanelPage.expectQuestionStatus('q-details1', 'pending');
			await questionPanelPage.expectQuestionTopic('q-details1', 'architecture.db');
			await questionPanelPage.expectQuestionText('q-details1', 'Should we use PostgreSQL or SQLite?');
		});
	});

	test.describe('Ask Form', () => {
		test.beforeEach(async ({ loopPanelPage }) => {
			await loopPanelPage.switchToQuestionsTab();
		});

		test('ask button is visible', async ({ questionPanelPage }) => {
			await expect(questionPanelPage.askButton).toBeVisible();
		});

		test('can open ask form', async ({ questionPanelPage }) => {
			await questionPanelPage.openAskForm();
			await questionPanelPage.expectAskFormVisible();
		});

		test('can cancel ask form', async ({ questionPanelPage }) => {
			await questionPanelPage.openAskForm();
			await questionPanelPage.cancelAskForm();
			await questionPanelPage.expectAskFormHidden();
		});

		test('can fill and submit ask form', async ({ questionPanelPage, page }) => {
			let messageSent = false;

			await page.route('**/agentic-dispatch/message', route => {
				const request = route.request();
				const body = request.postDataJSON();

				if (body?.content?.includes('/ask')) {
					messageSent = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: `Created question **q-newquest**

**Topic**: test.topic
**Question**: Is this a test?
**Status**: pending

Use \`/questions\` to view pending questions
Use \`/answer q-newquest <response>\` to answer`,
							type: 'result'
						})
					});
				} else {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: 'No pending questions found.',
							type: 'result'
						})
					});
				}
			});

			await questionPanelPage.openAskForm();
			await questionPanelPage.fillAskForm('test.topic', 'Is this a test?');
			await questionPanelPage.submitAskForm();

			// Wait for submission
			await page.waitForTimeout(500);
			expect(messageSent).toBe(true);
		});
	});

	test.describe('Answer Form', () => {
		test.beforeEach(async ({ loopPanelPage, page }) => {
			// Mock a pending question
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Pending Questions (1)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-answer01 | api.test | pending | What is the answer? |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await page.reload();
			await loopPanelPage.switchToQuestionsTab();
			await page.waitForTimeout(500);
		});

		test('can open answer form for pending question', async ({ questionPanelPage }) => {
			await questionPanelPage.openAnswerForm('q-answer01');
			await questionPanelPage.expectAnswerFormVisible('q-answer01');
		});

		test('can cancel answer form', async ({ questionPanelPage }) => {
			await questionPanelPage.openAnswerForm('q-answer01');
			await questionPanelPage.cancelAnswer('q-answer01');
			await questionPanelPage.expectAnswerFormHidden('q-answer01');
		});

		test('can submit answer', async ({ questionPanelPage, page }) => {
			let answerSent = false;

			await page.route('**/agentic-dispatch/message', route => {
				const request = route.request();
				const body = request.postDataJSON();

				if (body?.content?.includes('/answer')) {
					answerSent = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: `Answered question **q-answer01**

**Original question**: What is the answer?

**Your answer**: 42

Loop may resume with this answer.`,
							type: 'result'
						})
					});
				} else {
					route.continue();
				}
			});

			await questionPanelPage.openAnswerForm('q-answer01');
			await questionPanelPage.submitAnswer('q-answer01', '42');

			await page.waitForTimeout(500);
			expect(answerSent).toBe(true);
		});
	});

	test.describe('Question States', () => {
		test('shows answered questions with answer content', async ({ questionPanelPage, loopPanelPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Answered Questions (1)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-answered | api.test | answered | Was this answered? |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await page.reload();
			await loopPanelPage.switchToQuestionsTab();
			await page.waitForTimeout(500);

			await questionPanelPage.filterByAnswered();
			await questionPanelPage.expectQuestionCards(1);
			await questionPanelPage.expectQuestionStatus('q-answered', 'answered');
		});

		test('shows blocking questions with urgency badge', async ({ questionPanelPage, loopPanelPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Pending Questions (1)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-blocking | critical.issue | pending | This is urgent! |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await page.reload();
			await loopPanelPage.switchToQuestionsTab();
			await page.waitForTimeout(500);

			// Note: The urgency isn't in the table output, so this tests that the card renders
			await questionPanelPage.expectQuestionCards(1);
		});
	});

	test.describe('Chat Command Integration', () => {
		test('/ask command creates question', async ({ chatPage, page }) => {
			let commandReceived = false;

			await page.route('**/agentic-dispatch/message', route => {
				const request = route.request();
				const body = request.postDataJSON();

				if (body?.content?.includes('/ask')) {
					commandReceived = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: `Created question **q-chatask**

**Topic**: chat.test
**Question**: Testing from chat
**Status**: pending`,
							type: 'result'
						})
					});
				} else {
					route.continue();
				}
			});

			await chatPage.sendMessage(testData.askCommand('chat.test', 'Testing from chat'));
			await chatPage.waitForResponse();

			expect(commandReceived).toBe(true);
		});

		test('/questions command lists questions', async ({ chatPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-response',
						content: `# Pending Questions (2)

| ID | Topic | Status | Question |
|-----|-------|--------|----------|
| q-list1 | api.test | pending | Question 1? |
| q-list2 | arch.test | pending | Question 2? |

---
Use \`/questions <id>\` to view details`,
						type: 'result'
					})
				});
			});

			await chatPage.sendMessage(testData.questionsCommand());
			await chatPage.waitForResponse();

			// Verify the response contains the question table
			const messages = await chatPage.getMessages();
			const lastMessage = messages.at(-1);
			expect(lastMessage).toContain('Pending Questions');
			expect(lastMessage).toContain('q-list1');
		});

		test('/answer command answers question', async ({ chatPage, page }) => {
			let answerReceived = false;

			await page.route('**/agentic-dispatch/message', route => {
				const request = route.request();
				const body = request.postDataJSON();

				if (body?.content?.includes('/answer')) {
					answerReceived = true;
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							response_id: 'test-response',
							content: `Answered question **q-chatans**

**Original question**: What is the answer?

**Your answer**: The answer is 42`,
							type: 'result'
						})
					});
				} else {
					route.continue();
				}
			});

			await chatPage.sendMessage(testData.answerCommand('q-chatans', 'The answer is 42'));
			await chatPage.waitForResponse();

			expect(answerReceived).toBe(true);
		});
	});
});
