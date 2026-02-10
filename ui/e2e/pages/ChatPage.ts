import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Chat interface.
 *
 * Provides methods to interact with the chat view including:
 * - Sending messages
 * - Waiting for responses
 * - Verifying message content
 */
export class ChatPage {
	readonly page: Page;
	readonly messageInput: Locator;
	readonly sendButton: Locator;
	readonly messageList: Locator;
	readonly emptyState: Locator;

	constructor(page: Page) {
		this.page = page;
		this.messageInput = page.locator('textarea[placeholder="Type a message..."]');
		this.sendButton = page.locator('button[aria-label="Send message"]');
		this.messageList = page.locator('[role="log"][aria-label="Chat messages"]');
		// Scope empty state to the message list to avoid matching loop panel's empty state
		this.emptyState = this.messageList.locator('.empty-state');
	}

	async goto(): Promise<void> {
		await this.page.goto('/');
		await expect(this.messageList).toBeVisible();
	}

	async sendMessage(text: string): Promise<void> {
		await this.messageInput.fill(text);
		await this.sendButton.click();
	}

	async typeMessage(text: string): Promise<void> {
		await this.messageInput.fill(text);
	}

	async pressEnterToSend(): Promise<void> {
		// Use locator's press method which properly targets the element
		await this.messageInput.press('Enter');
	}

	async waitForResponse(timeout = 30000): Promise<void> {
		// Wait for a non-user message to appear (assistant, status, or error)
		await this.page.waitForSelector(
			'.message:not(.user)',
			{ timeout }
		);
	}

	async waitForMessageCount(count: number, timeout = 10000): Promise<void> {
		await expect(this.messageList.locator('.message')).toHaveCount(count, { timeout });
	}

	async expectMessageCount(count: number): Promise<void> {
		await expect(this.messageList.locator('.message')).toHaveCount(count);
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
		await expect(this.emptyState.locator('.empty-title')).toHaveText('Start a conversation');
	}

	async expectNoEmptyState(): Promise<void> {
		await expect(this.emptyState).not.toBeVisible();
	}

	async expectLastMessageContains(content: string): Promise<void> {
		const lastMessage = this.messageList.locator('.message').last();
		await expect(lastMessage.locator('.message-body')).toContainText(content);
	}

	async expectLastMessageType(type: 'user' | 'assistant' | 'status' | 'error'): Promise<void> {
		const lastMessage = this.messageList.locator('.message').last();
		// Check the author label which is the most reliable indicator
		const authorLabels: Record<string, string> = {
			user: 'You',
			assistant: 'Assistant',
			status: 'Status',
			error: 'Error'
		};
		await expect(lastMessage.locator('.message-author')).toHaveText(authorLabels[type]);
	}

	async expectUserMessage(content: string): Promise<void> {
		// Find messages with "You" as author (user messages) containing the content
		const userMessages = this.messageList.locator('.message').filter({
			has: this.page.locator('.message-author', { hasText: 'You' })
		});
		await expect(userMessages.filter({ hasText: content })).toBeVisible();
	}

	async expectErrorMessage(): Promise<void> {
		const errorMessages = this.messageList.locator('.message.error');
		await expect(errorMessages).toBeVisible();
	}

	async expectSendButtonDisabled(): Promise<void> {
		await expect(this.sendButton).toBeDisabled();
	}

	async expectSendButtonEnabled(): Promise<void> {
		await expect(this.sendButton).toBeEnabled();
	}

	async expectInputDisabled(): Promise<void> {
		await expect(this.messageInput).toBeDisabled();
	}

	async expectInputEnabled(): Promise<void> {
		await expect(this.messageInput).toBeEnabled();
	}

	async expectLoadingState(): Promise<void> {
		// When sending, the send button shows a loader icon
		const loaderIcon = this.sendButton.locator('[data-icon="loader"]');
		await expect(loaderIcon).toBeVisible();
	}

	async getMessageCount(): Promise<number> {
		return await this.messageList.locator('.message').count();
	}

	async getAllMessages(): Promise<{ type: string; content: string }[]> {
		const messages = await this.messageList.locator('.message').all();
		const results: { type: string; content: string }[] = [];

		for (const msg of messages) {
			const author = await msg.locator('.message-author').textContent();
			const content = await msg.locator('.message-body').textContent();
			const typeMap: Record<string, string> = {
				'You': 'user',
				'Assistant': 'assistant',
				'Status': 'status',
				'Error': 'error'
			};
			results.push({
				type: typeMap[author || ''] || 'unknown',
				content: content || ''
			});
		}

		return results;
	}
}
