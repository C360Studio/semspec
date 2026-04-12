import type { Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Plan Create Form (/plans/new)
// ---------------------------------------------------------------------------
export const planGoalInput = (page: Page) => page.getByLabel('What do you want to build?');

export const createPlanButton = (page: Page) =>
	page.getByRole('button', { name: /Create Plan/i });

// ---------------------------------------------------------------------------
// Action Bar (plan detail page)
// ---------------------------------------------------------------------------
export const approvePlanButton = (page: Page) =>
	page.getByRole('button', { name: /Create Requirements/i });

export const approveAndContinueButton = (page: Page) =>
	page.getByRole('button', { name: /Approve & Continue/i });

export const startExecutionButton = (page: Page) =>
	page.getByRole('button', { name: /Start Execution/i });

export const editPlanButton = (page: Page) =>
	page.getByRole('button', { name: /Edit/i });

export const replayButton = (page: Page) => page.getByRole('button', { name: /Replay/i });

// Status indicators (all use role="status" in ActionBar)
export const cascadeStatus = (page: Page, text?: string) => {
	const base = page.getByRole('status');
	return text ? base.filter({ hasText: text }) : base;
};

export const generatingRequirementsStatus = (page: Page) =>
	cascadeStatus(page, 'Generating requirements');

export const generatingScenariosStatus = (page: Page) =>
	cascadeStatus(page, 'Generating scenarios');

export const executingStatus = (page: Page) => cascadeStatus(page, 'Executing');

export const completeStatus = (page: Page) => cascadeStatus(page, 'Complete');

// ---------------------------------------------------------------------------
// Plan Detail Header
// ---------------------------------------------------------------------------
export const stageBadge = (page: Page, stage: string) =>
	page.locator(`[data-stage="${stage}"]`);

export const backLink = (page: Page) => page.getByRole('link', { name: /Back/i });

export const planTitle = (page: Page) => page.locator('h1');

// ---------------------------------------------------------------------------
// Error / Alert
// ---------------------------------------------------------------------------
export const errorAlert = (page: Page) => page.getByRole('alert');

// ---------------------------------------------------------------------------
// Left Panel
// ---------------------------------------------------------------------------
export const plansModeRadio = (page: Page) => page.getByRole('radio', { name: 'Plans' });

export const feedModeRadio = (page: Page) => page.getByRole('radio', { name: 'Feed' });

export const newPlanLink = (page: Page) => page.getByRole('link', { name: /New Plan/i });

export const filterChip = (page: Page, label: string) =>
	page.getByRole('radio', { name: label });

export const planListItem = (page: Page, slug: string) =>
	page.getByTestId('panel-left').getByRole('link', { name: slug });

export const emptyPlansMessage = (page: Page) => page.getByText('No plans');

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------
export const connectionStatus = (page: Page, text: string) => page.getByText(text);
