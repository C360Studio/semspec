import { type Page, type Locator, expect } from '@playwright/test';

/**
 * Page Object Model for the Setup Wizard.
 *
 * API Pattern:
 * - `get*` methods return Locators for external use
 * - `expect*` methods handle assertions internally
 * - Action methods (`click*`, `enter*`, `select*`) perform interactions
 *
 * Provides methods to interact with and verify:
 * - Scaffold step (greenfield projects)
 * - Detection step
 * - Checklist step
 * - Standards step
 * - Completion step with source suggestions
 * - Upload modal
 * - Accessibility attributes
 */
export class SetupWizardPage {
	readonly page: Page;
	readonly wizardOverlay: Locator;
	readonly wizardTitle: Locator;
	readonly stepIndicators: Locator;
	readonly stepDots: Locator;

	// Loading/detecting states
	readonly loadingState: Locator;
	readonly detectingState: Locator;
	readonly scaffoldingState: Locator;
	readonly initializingState: Locator;
	readonly errorState: Locator;

	// Scaffold step
	readonly scaffoldPanel: Locator;
	readonly scaffoldIntro: Locator;
	readonly languageSection: Locator;
	readonly languageCards: Locator;
	readonly frameworkSection: Locator;
	readonly frameworkCards: Locator;

	// Detection step
	readonly detectionPanel: Locator;
	readonly projectNameInput: Locator;
	readonly projectDescInput: Locator;
	readonly detectedLanguages: Locator;
	readonly detectedFrameworks: Locator;
	readonly detectedTooling: Locator;
	readonly existingDocs: Locator;

	// Checklist step
	readonly checklistPanel: Locator;
	readonly checkTable: Locator;
	readonly addCheckButton: Locator;

	// Standards step
	readonly standardsPanel: Locator;
	readonly ruleList: Locator;
	readonly addRuleButton: Locator;
	readonly generateFromDocsButton: Locator;

	// Completion step
	readonly completionPanel: Locator;
	readonly completionHeader: Locator;
	readonly readinessSection: Locator;
	readonly readinessList: Locator;
	readonly sourcesSection: Locator;
	readonly sourceSuggestions: Locator;
	readonly createPlanButton: Locator;
	readonly skipButton: Locator;

	// Upload modal
	readonly uploadModal: Locator;
	readonly uploadModalTitle: Locator;
	readonly uploadModalDropZone: Locator;
	readonly uploadModalCategoryButtons: Locator;
	readonly uploadModalCloseButton: Locator;

	// Footer
	readonly wizardFooter: Locator;
	readonly backButton: Locator;
	readonly nextButton: Locator;
	readonly createProjectButton: Locator;
	readonly initializeButton: Locator;
	readonly stepLabel: Locator;

	constructor(page: Page) {
		this.page = page;
		this.wizardOverlay = page.locator('.wizard-overlay');
		this.wizardTitle = page.locator('#wizard-title');
		this.stepIndicators = page.locator('.step-indicators');
		this.stepDots = page.locator('.step-dot');

		// Loading/detecting states
		this.loadingState = page.locator('.state-center').filter({ hasText: 'Checking project status' });
		this.detectingState = page.locator('.state-center').filter({ hasText: 'Scanning repository' });
		this.scaffoldingState = page.locator('.state-center').filter({ hasText: 'Creating project files' });
		this.initializingState = page.locator('.state-center').filter({ hasText: 'Initializing project' });
		this.errorState = page.locator('.error-state');

		// Scaffold step
		this.scaffoldPanel = page.locator('.panel').filter({ hasText: 'This looks like a new project' });
		this.scaffoldIntro = this.scaffoldPanel.locator('.panel-intro');
		this.languageSection = this.scaffoldPanel.locator('.section').filter({ hasText: 'Languages' });
		this.languageCards = this.languageSection.locator('.option-card');
		this.frameworkSection = this.scaffoldPanel.locator('.section').filter({ hasText: 'Frameworks' });
		this.frameworkCards = this.frameworkSection.locator('.option-card');

		// Detection step
		this.detectionPanel = page.locator('.panel').filter({ hasText: 'Welcome to Semspec!' });
		this.projectNameInput = page.locator('#project-name');
		this.projectDescInput = page.locator('#project-desc');
		this.detectedLanguages = this.detectionPanel.locator('.section').filter({ hasText: 'Languages' }).locator('.chip');
		this.detectedFrameworks = this.detectionPanel.locator('.section').filter({ hasText: 'Frameworks' }).locator('.chip');
		this.detectedTooling = this.detectionPanel.locator('.section').filter({ hasText: 'Tooling' }).locator('.chip');
		this.existingDocs = this.detectionPanel.locator('.section').filter({ hasText: 'Existing Documentation' }).locator('.doc-item');

		// Checklist step
		this.checklistPanel = page.locator('.panel').filter({ hasText: 'quality checks will run' });
		this.checkTable = page.locator('.check-table');
		this.addCheckButton = this.checklistPanel.locator('button', { hasText: 'Add Check' });

		// Standards step
		this.standardsPanel = page.locator('.panel').filter({ hasText: 'Coding standards are rules' });
		this.ruleList = page.locator('.rule-list');
		this.addRuleButton = this.standardsPanel.locator('button', { hasText: 'Add Rule' });
		this.generateFromDocsButton = this.standardsPanel.locator('button', { hasText: 'Generate from Docs' });

		// Completion step
		this.completionPanel = page.locator('.completion-panel');
		this.completionHeader = page.locator('.completion-header');
		this.readinessSection = page.locator('.readiness-section');
		this.readinessList = page.locator('.readiness-list');
		this.sourcesSection = page.locator('.sources-section');
		this.sourceSuggestions = page.locator('.suggestion-card');
		this.createPlanButton = page.locator('.cta-section .btn-primary');
		this.skipButton = page.locator('.skip-btn');

		// Upload modal
		this.uploadModal = page.locator('.modal').filter({ has: page.locator('#upload-title') });
		this.uploadModalTitle = page.locator('#upload-title');
		this.uploadModalDropZone = this.uploadModal.locator('.drop-zone');
		this.uploadModalCategoryButtons = this.uploadModal.locator('.category-option');
		this.uploadModalCloseButton = this.uploadModal.locator('.close-button');

		// Footer
		this.wizardFooter = page.locator('.wizard-footer');
		this.backButton = this.wizardFooter.locator('.btn-ghost', { hasText: 'Back' });
		this.nextButton = this.wizardFooter.locator('.btn-primary', { hasText: 'Next' });
		this.createProjectButton = this.wizardFooter.locator('.btn-primary', { hasText: 'Create Project' });
		this.initializeButton = this.wizardFooter.locator('.btn-primary', { hasText: 'Initialize Project' });
		this.stepLabel = this.wizardFooter.locator('.step-label');
	}

	// --- Navigation ---

	async goto(): Promise<void> {
		await this.page.goto('/');
		await this.waitForHydration();
	}

	async waitForHydration(): Promise<void> {
		await this.page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });
	}

	async reload(): Promise<void> {
		await this.page.reload();
		await this.waitForHydration();
	}

	// --- Step verification ---

	async expectWizardVisible(): Promise<void> {
		await expect(this.wizardOverlay).toBeVisible();
	}

	async expectWizardHidden(): Promise<void> {
		await expect(this.wizardOverlay).not.toBeVisible();
	}

	async expectStep(step: 'scaffold' | 'detection' | 'checklist' | 'standards' | 'complete'): Promise<void> {
		switch (step) {
			case 'scaffold':
				await expect(this.scaffoldPanel).toBeVisible();
				break;
			case 'detection':
				await expect(this.detectionPanel).toBeVisible();
				break;
			case 'checklist':
				await expect(this.checklistPanel).toBeVisible();
				break;
			case 'standards':
				await expect(this.standardsPanel).toBeVisible();
				break;
			case 'complete':
				await expect(this.completionPanel).toBeVisible();
				break;
		}
	}

	async expectLoadingState(): Promise<void> {
		await expect(this.loadingState).toBeVisible();
	}

	async expectDetectingState(): Promise<void> {
		await expect(this.detectingState).toBeVisible();
	}

	async expectScaffoldingState(): Promise<void> {
		await expect(this.scaffoldingState).toBeVisible();
	}

	async expectErrorState(message?: string): Promise<void> {
		await expect(this.errorState).toBeVisible();
		if (message) {
			await expect(this.errorState).toContainText(message);
		}
	}

	// --- Scaffold step methods ---

	getLanguageCard(name: string): Locator {
		return this.languageCards.filter({ hasText: name });
	}

	async selectLanguage(name: string): Promise<void> {
		const card = this.getLanguageCard(name);
		await card.click();
	}

	async deselectLanguage(name: string): Promise<void> {
		const card = this.getLanguageCard(name);
		await expect(card).toHaveClass(/selected/);
		await card.click();
	}

	async expectLanguageSelected(name: string): Promise<void> {
		const card = this.getLanguageCard(name);
		await expect(card).toHaveClass(/selected/);
	}

	async expectLanguageNotSelected(name: string): Promise<void> {
		const card = this.getLanguageCard(name);
		await expect(card).not.toHaveClass(/selected/);
	}

	getFrameworkCard(name: string): Locator {
		return this.frameworkCards.filter({ hasText: name });
	}

	async selectFramework(name: string): Promise<void> {
		const card = this.getFrameworkCard(name);
		await card.click();
	}

	async expectFrameworkVisible(name: string): Promise<void> {
		const card = this.getFrameworkCard(name);
		await expect(card).toBeVisible();
	}

	async expectFrameworkHidden(name: string): Promise<void> {
		await expect(this.frameworkSection).not.toBeVisible();
	}

	async expectFrameworkSelected(name: string): Promise<void> {
		const card = this.getFrameworkCard(name);
		await expect(card).toHaveClass(/selected/);
	}

	async clickCreateProject(): Promise<void> {
		await this.createProjectButton.click();
	}

	async expectCreateProjectButtonDisabled(): Promise<void> {
		await expect(this.createProjectButton).toBeDisabled();
	}

	async expectCreateProjectButtonEnabled(): Promise<void> {
		await expect(this.createProjectButton).toBeEnabled();
	}

	// --- Detection step methods ---

	async enterProjectName(name: string): Promise<void> {
		await this.projectNameInput.fill(name);
	}

	async clearProjectName(): Promise<void> {
		await this.projectNameInput.clear();
	}

	async enterProjectDescription(desc: string): Promise<void> {
		await this.projectDescInput.fill(desc);
	}

	async expectDetectedLanguage(name: string): Promise<void> {
		const chip = this.detectedLanguages.filter({ hasText: name });
		await expect(chip).toBeVisible();
	}

	async expectDetectedFramework(name: string): Promise<void> {
		const chip = this.detectedFrameworks.filter({ hasText: name });
		await expect(chip).toBeVisible();
	}

	// --- Navigation methods ---

	async clickBack(): Promise<void> {
		await this.backButton.click();
	}

	async clickNext(): Promise<void> {
		await this.nextButton.click();
	}

	async expectBackButtonDisabled(): Promise<void> {
		await expect(this.backButton).toBeDisabled();
	}

	async expectBackButtonEnabled(): Promise<void> {
		await expect(this.backButton).toBeEnabled();
	}

	async expectNextButtonDisabled(): Promise<void> {
		await expect(this.nextButton).toBeDisabled();
	}

	async expectNextButtonEnabled(): Promise<void> {
		await expect(this.nextButton).toBeEnabled();
	}

	async expectStepCount(count: number): Promise<void> {
		await expect(this.stepDots).toHaveCount(count);
	}

	async expectCurrentStepIndex(index: number): Promise<void> {
		const activeStep = this.stepDots.nth(index);
		await expect(activeStep).toHaveClass(/active/);
	}

	// --- Completion step methods ---

	async expectReadinessItem(text: string, done: boolean): Promise<void> {
		const item = this.readinessList.locator('li').filter({ hasText: text });
		await expect(item).toBeVisible();
		if (done) {
			await expect(item).toHaveClass(/done/);
		} else {
			await expect(item).not.toHaveClass(/done/);
		}
	}

	async expectSourceSuggestion(label: string): Promise<void> {
		const card = this.sourceSuggestions.filter({ hasText: label });
		await expect(card).toBeVisible();
	}

	async clickSourceSuggestion(label: string): Promise<void> {
		const card = this.sourceSuggestions.filter({ hasText: label });
		await card.click();
	}

	async expectSuggestionUploaded(label: string): Promise<void> {
		const card = this.sourceSuggestions.filter({ hasText: label });
		await expect(card).toHaveClass(/uploaded/);
		await expect(card).toBeDisabled();
	}

	async clickCreatePlan(): Promise<void> {
		await this.createPlanButton.click();
	}

	async clickSkip(): Promise<void> {
		await this.skipButton.click();
	}

	// --- Upload modal methods ---

	async expectUploadModalOpen(): Promise<void> {
		await expect(this.uploadModal).toBeVisible();
	}

	async expectUploadModalClosed(): Promise<void> {
		await expect(this.uploadModal).not.toBeVisible();
	}

	async expectUploadModalCategory(category: string): Promise<void> {
		const selectedCategory = this.uploadModalCategoryButtons.filter({ hasText: category }).filter({ hasClass: /selected/ });
		await expect(selectedCategory).toBeVisible();
	}

	async closeUploadModal(): Promise<void> {
		await this.uploadModalCloseButton.click();
	}

	// --- Initialize step ---

	async clickInitialize(): Promise<void> {
		await this.initializeButton.click();
	}

	// --- Accessibility verification methods ---

	async expectWizardHasDialogRole(): Promise<void> {
		await expect(this.wizardOverlay).toHaveAttribute('role', 'dialog');
		await expect(this.wizardOverlay).toHaveAttribute('aria-modal', 'true');
		await expect(this.wizardOverlay).toHaveAttribute('aria-labelledby', 'wizard-title');
	}

	async expectStateHasStatusRole(stateLocator: Locator): Promise<void> {
		await expect(stateLocator).toHaveAttribute('role', 'status');
		await expect(stateLocator).toHaveAttribute('aria-live', 'polite');
	}

	async expectErrorStateHasAlertRole(): Promise<void> {
		await expect(this.errorState).toHaveAttribute('role', 'alert');
	}

	async expectStepHasAriaCurrent(index: number): Promise<void> {
		const step = this.stepDots.nth(index);
		await expect(step).toHaveAttribute('aria-current', 'step');
	}

	async expectButtonHasAriaExpanded(button: Locator, expanded: boolean): Promise<void> {
		await expect(button).toHaveAttribute('aria-expanded', String(expanded));
	}

	// --- Focus verification methods ---

	async getFocusedElementTag(): Promise<string> {
		return await this.page.evaluate(() => document.activeElement?.tagName ?? 'NONE');
	}

	async getFocusedElementText(): Promise<string> {
		return await this.page.evaluate(() => document.activeElement?.textContent ?? '');
	}

	async isFocusWithinWizard(): Promise<boolean> {
		return await this.page.evaluate(() => {
			const wizard = document.querySelector('.wizard-overlay');
			return wizard?.contains(document.activeElement) ?? false;
		});
	}

	async pressTab(): Promise<void> {
		await this.page.keyboard.press('Tab');
	}

	async pressShiftTab(): Promise<void> {
		await this.page.keyboard.press('Shift+Tab');
	}

	async pressEnter(): Promise<void> {
		await this.page.keyboard.press('Enter');
	}

	async pressSpace(): Promise<void> {
		await this.page.keyboard.press('Space');
	}

	async pressEscape(): Promise<void> {
		await this.page.keyboard.press('Escape');
	}
}
