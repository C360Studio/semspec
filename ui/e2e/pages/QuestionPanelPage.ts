import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Question Panel.
 *
 * Provides methods to interact with and verify:
 * - Question list and filtering
 * - Question cards with status and topic
 * - Answer form functionality
 * - Ask form for creating questions
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
	readonly askButton: Locator;
	readonly askForm: Locator;
	readonly refreshButton: Locator;

	constructor(page: Page) {
		this.page = page;
		// QuestionPanel is rendered inside the loop-panel when Questions tab is active
		this.panel = page.locator('.question-panel');
		this.filterTabs = this.panel.locator('.filter-tabs');
		this.pendingTab = this.filterTabs.locator('.tab').filter({ hasText: 'Pending' });
		this.answeredTab = this.filterTabs.locator('.tab').filter({ hasText: 'Answered' });
		this.allTab = this.filterTabs.locator('.tab').filter({ hasText: 'All' });
		this.questionList = this.panel.locator('.question-list');
		this.questionCards = this.panel.locator('.question-card');
		this.emptyState = this.panel.locator('.empty-state');
		this.askButton = this.panel.locator('.ask-btn');
		this.askForm = this.panel.locator('.ask-form');
		this.refreshButton = this.panel.locator('.refresh-btn');
	}

	// Panel state
	async expectVisible(): Promise<void> {
		await expect(this.panel).toBeVisible();
	}

	async expectFilterTabsVisible(): Promise<void> {
		await expect(this.filterTabs).toBeVisible();
		await expect(this.pendingTab).toBeVisible();
		await expect(this.answeredTab).toBeVisible();
		await expect(this.allTab).toBeVisible();
	}

	// Filter navigation
	async filterByPending(): Promise<void> {
		await this.pendingTab.click();
	}

	async filterByAnswered(): Promise<void> {
		await this.answeredTab.click();
	}

	async filterByAll(): Promise<void> {
		await this.allTab.click();
	}

	async expectPendingFilterActive(): Promise<void> {
		await expect(this.pendingTab).toHaveClass(/active/);
	}

	async expectAnsweredFilterActive(): Promise<void> {
		await expect(this.answeredTab).toHaveClass(/active/);
	}

	async expectAllFilterActive(): Promise<void> {
		await expect(this.allTab).toHaveClass(/active/);
	}

	async expectPendingBadge(count: number): Promise<void> {
		const badge = this.pendingTab.locator('.tab-count');
		if (count > 0) {
			await expect(badge).toBeVisible();
			await expect(badge).toHaveText(String(count));
		} else {
			await expect(badge).not.toBeVisible();
		}
	}

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
		const statusBadge = card.locator('.status-badge');
		await expect(statusBadge).toHaveText(status);
	}

	async expectQuestionTopic(questionId: string, topic: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const topicElement = card.locator('.question-topic');
		await expect(topicElement).toContainText(topic);
	}

	async expectQuestionText(questionId: string, text: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const questionText = card.locator('.question-text');
		await expect(questionText).toContainText(text);
	}

	async expectQuestionUrgency(questionId: string, urgency: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const urgencyBadge = card.locator('.urgency-badge');
		await expect(urgencyBadge).toContainText(urgency);
	}

	// Answer functionality
	async openAnswerForm(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const answerButton = card.locator('.action-btn.answer');
		await answerButton.click();
	}

	async submitAnswer(questionId: string, answer: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const textarea = card.locator('.answer-form textarea');
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
		const answerForm = card.locator('.answer-form');
		await expect(answerForm).toBeVisible();
	}

	async expectAnswerFormHidden(questionId: string): Promise<void> {
		const card = await this.getQuestionCard(questionId);
		const answerForm = card.locator('.answer-form');
		await expect(answerForm).not.toBeVisible();
	}

	// Ask functionality
	async openAskForm(): Promise<void> {
		await this.askButton.click();
	}

	async expectAskFormVisible(): Promise<void> {
		await expect(this.askForm).toBeVisible();
	}

	async expectAskFormHidden(): Promise<void> {
		await expect(this.askForm).not.toBeVisible();
	}

	async fillAskForm(topic: string, question: string): Promise<void> {
		const topicInput = this.askForm.locator('#ask-topic');
		const questionInput = this.askForm.locator('#ask-question');
		await topicInput.fill(topic);
		await questionInput.fill(question);
	}

	async submitAskForm(): Promise<void> {
		const submitButton = this.askForm.locator('.btn-submit');
		await submitButton.click();
	}

	async cancelAskForm(): Promise<void> {
		const cancelButton = this.askForm.locator('.btn-cancel');
		await cancelButton.click();
	}

	// Refresh
	async refresh(): Promise<void> {
		await this.refreshButton.click();
	}
}
