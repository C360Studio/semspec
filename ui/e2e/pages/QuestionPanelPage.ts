import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Question Panel.
 *
 * Questions are rendered as QuestionMessage components (.question-message)
 * within the chat message log ([role="log"]). There is no dedicated
 * .question-panel container — questions appear inline in the chat.
 *
 * Previously questions had a separate panel UI with filter tabs.
 * That UI has been replaced by the QuestionMessage inline component.
 *
 * Provides methods to interact with and verify:
 * - Individual question messages in the chat log
 * - Question status (pending, answered, timeout)
 * - Answer form within question messages
 *
 * Note: Questions are created by agents via context-builder.
 * Humans can only view and answer questions.
 */
export class QuestionPanelPage {
	readonly page: Page;
	readonly panel: Locator;
	readonly filterTabs: Locator;
	readonly pendingTab: Locator;
	readonly answeredTab: Locator;
	readonly allTab: Locator;
	readonly questionList: Locator;
	readonly questionCards: Locator;
	readonly emptyState: Locator;
	readonly refreshButton: Locator;

	constructor(page: Page) {
		this.page = page;
		// Questions are rendered as .question-message elements inside the chat log
		// The parent container is the message log [role="log"]
		this.panel = page.locator('[role="log"][aria-label="Chat messages"]');
		// No filter tabs in the new inline QuestionMessage UI
		this.filterTabs = page.locator('[data-removed="question-filter-tabs-removed"]');
		this.pendingTab = page.locator('[data-removed="question-pending-tab-removed"]');
		this.answeredTab = page.locator('[data-removed="question-answered-tab-removed"]');
		this.allTab = page.locator('[data-removed="question-all-tab-removed"]');
		// Question list is the message log itself
		this.questionList = page.locator('[role="log"][aria-label="Chat messages"]');
		// Question cards are .question-message elements
		this.questionCards = page.locator('.question-message');
		// Empty state is the .empty-state inside the message log
		this.emptyState = this.panel.locator('.empty-state');
		// No refresh button in the inline QuestionMessage UI
		this.refreshButton = page.locator('[data-removed="question-refresh-btn-removed"]');
	}

	// Panel state
	async expectVisible(): Promise<void> {
		await expect(this.questionCards.first()).toBeVisible();
	}

	async expectFilterTabsVisible(): Promise<void> {
		// No filter tabs in new UI — this is a no-op for backwards compat
	}

	// Filter navigation — no-op in new inline UI
	async filterByPending(): Promise<void> {}
	async filterByAnswered(): Promise<void> {}
	async filterByAll(): Promise<void> {}

	async expectPendingFilterActive(): Promise<void> {}
	async expectAnsweredFilterActive(): Promise<void> {}
	async expectAllFilterActive(): Promise<void> {}

	async expectPendingBadge(_count: number): Promise<void> {}

	// Question cards
	async expectQuestionCards(count: number): Promise<void> {
		await expect(this.questionCards).toHaveCount(count);
	}

	async expectEmptyState(): Promise<void> {
		await expect(this.emptyState).toBeVisible();
	}

	async expectNoEmptyState(): Promise<void> {
		await expect(this.emptyState).not.toBeVisible();
	}

	async getQuestionCard(questionId: string): Promise<Locator> {
		return this.questionCards.filter({ hasText: questionId.slice(0, 10) });
	}

	async expectQuestionStatus(questionId: string, status: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Status is reflected via CSS class on the .question-message element
		await expect(card).toHaveClass(new RegExp(status));
	}

	async expectQuestionTopic(questionId: string, topic: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Topic is rendered in .topic span inside .question-header
		const topicElement = card.locator('.topic');
		await expect(topicElement).toContainText(topic);
	}

	async expectQuestionText(questionId: string, text: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Question text is in .question-text
		const questionText = card.locator('.question-text');
		await expect(questionText).toContainText(text);
	}

	async expectQuestionUrgency(questionId: string, urgency: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Urgency is reflected as a CSS class on the .question-message element
		await expect(card).toHaveClass(new RegExp(urgency));
	}

	// Answer functionality
	async openAnswerForm(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Reply button is .action-btn.reply
		const replyButton = card.locator('.action-btn.reply');
		await replyButton.click();
	}

	async submitAnswer(questionId: string, answer: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		// Reply form is .reply-form inside the question card
		const textarea = card.locator('.reply-form textarea');
		const submitButton = card.locator('.btn-submit');
		await textarea.fill(answer);
		await submitButton.click();
	}

	async cancelAnswer(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const cancelButton = card.locator('.btn-cancel');
		await cancelButton.click();
	}

	async expectAnswerFormVisible(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const answerForm = card.locator('.reply-form');
		await expect(answerForm).toBeVisible();
	}

	async expectAnswerFormHidden(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const answerForm = card.locator('.reply-form');
		await expect(answerForm).not.toBeVisible();
	}

	// Refresh — no-op in new inline UI
	async refresh(): Promise<void> {}
}
