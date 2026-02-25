import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Chat interface on the Activity page.
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

	// Source suggestion chip
	readonly suggestionChip: Locator;
	readonly suggestionChipValue: Locator;
	readonly suggestionChipAddButton: Locator;
	readonly suggestionChipDismissButton: Locator;

	// Upload modal
	readonly uploadModal: Locator;
	readonly uploadModalDropZone: Locator;
	readonly uploadModalFileInput: Locator;
	readonly uploadModalCategoryButtons: Locator;
	readonly uploadModalUploadButton: Locator;
	readonly uploadModalCancelButton: Locator;

	constructor(page: Page) {
		this.page = page;
		// Match textarea by aria-label which is more stable than placeholder text
		this.messageInput = page.locator('textarea[aria-label="Message input"]');
		this.sendButton = page.locator('button[aria-label="Send message"]');
		this.messageList = page.locator('[role="log"][aria-label="Chat messages"]');
		// Scope empty state to the message list to avoid matching loop panel's empty state
		this.emptyState = this.messageList.locator('.empty-state');

		// Source suggestion chip
		this.suggestionChip = page.locator('.chip[role="group"]');
		this.suggestionChipValue = this.suggestionChip.locator('.value');
		this.suggestionChipAddButton = this.suggestionChip.locator('.action-button.primary');
		this.suggestionChipDismissButton = this.suggestionChip.locator('.action-button.dismiss');

		// Upload modal
		this.uploadModal = page.locator('.modal[aria-labelledby="upload-title"]');
		this.uploadModalDropZone = this.uploadModal.locator('.drop-zone');
		this.uploadModalFileInput = this.uploadModal.locator('input[type="file"]');
		this.uploadModalCategoryButtons = this.uploadModal.locator('.category-option');
		this.uploadModalUploadButton = this.uploadModal.locator('.btn-primary');
		this.uploadModalCancelButton = this.uploadModal.locator('.btn-secondary');
	}

	async goto(): Promise<void> {
		await this.page.goto('/activity');
		// Wait for SvelteKit hydration to complete before interacting
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
		// Open the chat drawer via keyboard shortcut (Cmd+K on Mac, Ctrl+K on Windows/Linux)
		await this.openDrawer();
		await expect(this.messageList).toBeVisible();
	}

	/**
	 * Open the chat drawer via keyboard shortcut.
	 */
	async openDrawer(): Promise<void> {
		const isMac = process.platform === 'darwin';
		await this.page.keyboard.press(isMac ? 'Meta+k' : 'Control+k');
		// Wait for drawer to be visible
		await expect(this.page.locator('.chat-drawer')).toBeVisible();
	}

	/**
	 * Close the chat drawer.
	 */
	async closeDrawer(): Promise<void> {
		await this.page.keyboard.press('Escape');
		await expect(this.page.locator('.chat-drawer')).not.toBeVisible();
	}

	/**
	 * Send a message via the chat interface.
	 *
	 * Uses Playwright's fill() which sets the value AND fires input/change events.
	 * This works correctly when the page has no runtime errors blocking Svelte 5 reactivity.
	 */
	async sendMessage(text: string): Promise<void> {
		await this.messageInput.fill(text);
		// Wait for Svelte 5 reactivity to process the input event and enable the send button
		await expect(this.sendButton).toBeEnabled({ timeout: 5000 });
		await this.sendButton.click();
	}

	/**
	 * Type text into the message input.
	 *
	 * Uses fill() for reliability. For empty text, clears the input.
	 */
	async typeMessage(text: string): Promise<void> {
		if (text === '') {
			await this.messageInput.fill('');
			await expect(this.messageInput).toHaveValue('');
			return;
		}

		await this.messageInput.fill(text);
		await expect(this.messageInput).toHaveValue(text);
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

	// Source suggestion chip methods
	async expectSuggestionChip(type: 'url' | 'file'): Promise<void> {
		await expect(this.suggestionChip).toBeVisible();
		// lucide-svelte renders icons with class like "lucide-globe", "lucide-file"
		const iconClass = type === 'url' ? '.lucide-globe' : '.lucide-file';
		await expect(this.suggestionChip.locator(iconClass)).toBeVisible();
	}

	async expectSuggestionChipValue(value: string): Promise<void> {
		await expect(this.suggestionChipValue).toContainText(value);
	}

	async expectNoSuggestionChip(): Promise<void> {
		await expect(this.suggestionChip).not.toBeVisible();
	}

	async clickAddSource(): Promise<void> {
		await this.suggestionChipAddButton.click();
	}

	async dismissSuggestionChip(): Promise<void> {
		await this.suggestionChipDismissButton.click();
	}

	async waitForSuggestionChipLoading(): Promise<void> {
		await expect(this.suggestionChipAddButton.locator('.lucide-loader-2')).toBeVisible();
	}

	async waitForSuggestionChipNotLoading(): Promise<void> {
		await expect(this.suggestionChipAddButton.locator('.lucide-loader-2')).not.toBeVisible();
	}

	// Upload modal methods
	async expectUploadModalVisible(): Promise<void> {
		await expect(this.uploadModal).toBeVisible();
	}

	async expectUploadModalHidden(): Promise<void> {
		await expect(this.uploadModal).not.toBeVisible();
	}

	async closeUploadModal(): Promise<void> {
		await this.uploadModalCancelButton.click();
	}

	async selectCategory(category: 'reference' | 'sop' | 'spec' | 'api'): Promise<void> {
		const categoryButton = this.uploadModalCategoryButtons.filter({ hasText: new RegExp(category, 'i') });
		await categoryButton.click();
	}

	async uploadFile(filePath: string): Promise<void> {
		await this.uploadModalFileInput.setInputFiles(filePath);
	}

	async clickUploadButton(): Promise<void> {
		await this.uploadModalUploadButton.click();
	}

	// Status message helper
	async expectStatusMessage(content: string): Promise<void> {
		const statusMessages = this.messageList.locator('.message').filter({
			has: this.page.locator('.message-author', { hasText: 'Status' })
		});
		await expect(statusMessages.filter({ hasText: content })).toBeVisible();
	}

	// Mode indicator helpers
	async expectMode(mode: 'chat' | 'plan' | 'execute'): Promise<void> {
		const modeIndicator = this.page.locator('[data-testid="mode-indicator"]');
		await expect(modeIndicator).toBeVisible();
		await expect(modeIndicator).toHaveAttribute('data-mode', mode);
	}

	async expectModeLabel(label: string): Promise<void> {
		const modeIndicator = this.page.locator('[data-testid="mode-indicator"]');
		await expect(modeIndicator).toContainText(label);
	}
}
