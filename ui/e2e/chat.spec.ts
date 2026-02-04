import { test, expect, testData } from './helpers/setup';

test.describe('Chat Interface', () => {
	test.beforeEach(async ({ chatPage }) => {
		await chatPage.goto();
	});

	test.describe('Initial State', () => {
		test('shows empty state when no messages', async ({ chatPage }) => {
			await chatPage.expectEmptyState();
			await chatPage.expectMessageCount(0);
		});

		test('has enabled input and disabled send button initially', async ({ chatPage }) => {
			await chatPage.expectInputEnabled();
			await chatPage.expectSendButtonDisabled();
		});
	});

	test.describe('Message Input', () => {
		test('enables send button when text is entered', async ({ chatPage }) => {
			await chatPage.expectSendButtonDisabled();
			await chatPage.typeMessage('Hello');
			await chatPage.expectSendButtonEnabled();
		});

		test('disables send button when input is cleared', async ({ chatPage }) => {
			await chatPage.typeMessage('Hello');
			await chatPage.expectSendButtonEnabled();
			await chatPage.typeMessage('');
			await chatPage.expectSendButtonDisabled();
		});

		test('disables send button with only whitespace', async ({ chatPage }) => {
			await chatPage.typeMessage('   ');
			await chatPage.expectSendButtonDisabled();
		});
	});

	test.describe('Sending Messages', () => {
		test('sends message and shows user message immediately', async ({ chatPage }) => {
			const message = testData.simpleMessage();
			await chatPage.sendMessage(message);

			await chatPage.expectNoEmptyState();
			await chatPage.expectUserMessage(message);
		});

		test('sends message with Enter key', async ({ chatPage, page }) => {
			const message = 'Message sent with Enter';
			// Wait for input to be ready
			await chatPage.expectInputEnabled();
			// Click the input, then type and send with Enter in one sequence
			await chatPage.messageInput.click();
			// Use fill + click send as primary flow, then verify Enter works via keyboard
			await page.keyboard.type(message, { delay: 30 });
			await page.keyboard.press('Enter');

			await chatPage.expectUserMessage(message);
		});

		test('clears input after sending', async ({ chatPage }) => {
			const message = testData.simpleMessage();
			await chatPage.sendMessage(message);

			await expect(chatPage.messageInput).toHaveValue('');
		});

		test('receives response after sending message', async ({ chatPage }) => {
			await chatPage.sendMessage(testData.statusCommand());
			await chatPage.waitForResponse();

			// Should have user message + response
			const count = await chatPage.getMessageCount();
			expect(count).toBeGreaterThanOrEqual(2);
		});
	});

	test.describe('Message Display', () => {
		test('displays user messages with correct styling', async ({ chatPage }) => {
			await chatPage.sendMessage('User message test');
			// Verify user message appears with correct author label
			await chatPage.expectUserMessage('User message test');
		});

		test('displays assistant responses with correct author', async ({ chatPage }) => {
			await chatPage.sendMessage(testData.statusCommand());
			await chatPage.waitForResponse();

			const messages = await chatPage.getAllMessages();
			const nonUserMessages = messages.filter(m => m.type !== 'user');
			expect(nonUserMessages.length).toBeGreaterThan(0);
		});
	});

	test.describe('Error Handling', () => {
		test('displays error message on API failure', async ({ chatPage, page }) => {
			// Mock a failed API response
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Internal server error' })
				});
			});

			await chatPage.sendMessage('This will fail');
			await chatPage.waitForResponse();
			await chatPage.expectErrorMessage();
		});

		test('error messages have correct styling', async ({ chatPage, page }) => {
			await page.route('**/agentic-dispatch/message', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Server error' })
				});
			});

			await chatPage.sendMessage('Trigger error');
			await chatPage.waitForResponse();
			await chatPage.expectLastMessageType('error');
		});
	});

	test.describe('Loading State', () => {
		test('disables input while sending', async ({ chatPage, page }) => {
			// Delay the response to observe loading state
			await page.route('**/agentic-dispatch/message', async route => {
				await new Promise(resolve => setTimeout(resolve, 1000));
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						response_id: 'test-id',
						type: 'chat_response',
						content: 'Response',
						timestamp: new Date().toISOString()
					})
				});
			});

			await chatPage.typeMessage('Test message');
			await chatPage.sendButton.click();

			// Input should be disabled while sending
			await chatPage.expectInputDisabled();

			// Wait for response
			await chatPage.waitForResponse();

			// Input should be re-enabled after response
			await chatPage.expectInputEnabled();
		});
	});
});
