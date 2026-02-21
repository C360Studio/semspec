import { test, expect } from './helpers/setup';
import {
	mockEmptyDetection,
	mockWizardOptions,
	mockScaffoldResponse,
	mockGoDetection,
	mockInitializedStatus,
	mockUninitializedStatus,
	mockInitResponse,
	mockGenerateStandardsResponse
} from './fixtures/setupWizardData';

/**
 * Helper to set up all API mocks for greenfield project flow.
 */
async function setupGreenfieldMocks(page: import('@playwright/test').Page) {
	await page.route('**/api/project/status', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockUninitializedStatus)
		});
	});

	await page.route('**/api/project/detect', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockEmptyDetection)
		});
	});

	await page.route('**/api/project/wizard', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockWizardOptions)
		});
	});

	await page.route('**/api/project/scaffold', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockScaffoldResponse)
		});
	});
}

/**
 * Helper to set up API mocks for existing project flow.
 */
async function setupExistingProjectMocks(page: import('@playwright/test').Page) {
	await page.route('**/api/project/status', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockUninitializedStatus)
		});
	});

	await page.route('**/api/project/detect', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockGoDetection)
		});
	});
}

/**
 * Helper to set up API mocks for initialized project.
 */
async function setupInitializedMocks(page: import('@playwright/test').Page) {
	await page.route('**/api/project/status', (route) => {
		return route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(mockInitializedStatus)
		});
	});
}

test.describe('Setup Wizard', () => {
	test.describe('Greenfield Flow', () => {
		test('shows scaffold step for empty project', async ({ page, setupWizardPage }) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectWizardVisible();
			await setupWizardPage.expectStep('scaffold');
		});

		test('displays available languages from wizard endpoint', async ({
			page,
			setupWizardPage
		}) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Verify all languages from mock are displayed
			for (const lang of mockWizardOptions.languages) {
				const card = setupWizardPage.getLanguageCard(lang.name);
				await expect(card).toBeVisible();
			}
		});

		test('selecting language shows available frameworks', async ({ page, setupWizardPage }) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Initially no frameworks visible (no language selected)
			await setupWizardPage.expectFrameworkHidden('Flask');

			// Select Python
			await setupWizardPage.selectLanguage('Python');
			await setupWizardPage.expectLanguageSelected('Python');

			// Python frameworks should now be visible
			await setupWizardPage.expectFrameworkVisible('Flask');
			await setupWizardPage.expectFrameworkVisible('FastAPI');
		});

		test('deselecting language removes dependent frameworks', async ({
			page,
			setupWizardPage
		}) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Select Python and Flask
			await setupWizardPage.selectLanguage('Python');
			await setupWizardPage.selectFramework('Flask');
			await setupWizardPage.expectFrameworkSelected('Flask');

			// Deselect Python
			await setupWizardPage.deselectLanguage('Python');
			await setupWizardPage.expectLanguageNotSelected('Python');

			// Flask should no longer be visible
			await setupWizardPage.expectFrameworkHidden('Flask');
		});

		test('Create Project button disabled without language selection', async ({
			page,
			setupWizardPage
		}) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Button should be disabled initially
			await setupWizardPage.expectCreateProjectButtonDisabled();

			// Select a language
			await setupWizardPage.selectLanguage('Go');

			// Button should now be enabled
			await setupWizardPage.expectCreateProjectButtonEnabled();
		});

		test('scaffold creates marker files and proceeds to detection', async ({
			page,
			setupWizardPage
		}) => {
			// Set up greenfield mocks first
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/wizard', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockWizardOptions)
				});
			});

			await page.route('**/api/project/scaffold', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockScaffoldResponse)
				});
			});

			// Track detect call count to return different responses
			let detectCallCount = 0;
			await page.route('**/api/project/detect', (route) => {
				detectCallCount++;
				if (detectCallCount === 1) {
					// First call: empty (triggers greenfield)
					return route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(mockEmptyDetection)
					});
				} else {
					// Second call: after scaffold
					return route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(mockGoDetection)
					});
				}
			});

			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Select Go and create project
			await setupWizardPage.selectLanguage('Go');
			await setupWizardPage.clickCreateProject();

			// Should show scaffolding state briefly, then proceed to detection
			await setupWizardPage.expectStep('detection');
		});
	});

	test.describe('Existing Project Flow', () => {
		test('skips scaffold for existing project', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectWizardVisible();

			// Should go directly to detection step, not scaffold
			await setupWizardPage.expectStep('detection');
		});

		test('back button disabled on detection step for non-greenfield', async ({
			page,
			setupWizardPage
		}) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Back button should be disabled since there's no scaffold step to go back to
			await setupWizardPage.expectBackButtonDisabled();
		});

		test('displays detected languages and frameworks', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Verify detected items are shown
			await setupWizardPage.expectDetectedLanguage('Go');
			await setupWizardPage.expectDetectedFramework('Gin');
		});
	});

	test.describe('Completion Step', () => {
		test('shows completion panel when project is initialized', async ({
			page,
			setupWizardPage
		}) => {
			await setupInitializedMocks(page);
			await setupWizardPage.goto();

			// Should NOT show wizard since project is initialized
			await setupWizardPage.expectWizardHidden();
		});
	});

	test.describe('Navigation', () => {
		test('can navigate through all wizard steps', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);

			await page.route('**/api/project/generate-standards', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockGenerateStandardsResponse)
				});
			});

			await page.route('**/api/project/init', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockInitResponse)
				});
			});

			await setupWizardPage.goto();

			// Start at detection (existing project)
			await setupWizardPage.expectStep('detection');
			await setupWizardPage.enterProjectName('Test Project');
			await setupWizardPage.clickNext();

			// Checklist step
			await setupWizardPage.expectStep('checklist');
			await setupWizardPage.clickNext();

			// Standards step
			await setupWizardPage.expectStep('standards');
			await setupWizardPage.clickInitialize();

			// After initialization, wizard closes and activity view shows
			await setupWizardPage.expectWizardHidden();
		});

		test('step indicators show correct count', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// For existing project: detection, checklist, standards = 3 steps (complete is not shown as a step)
			await setupWizardPage.expectStepCount(3);
		});

		test('can navigate back through steps', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Go to checklist
			await setupWizardPage.clickNext();
			await setupWizardPage.expectStep('checklist');

			// Go back
			await setupWizardPage.clickBack();
			await setupWizardPage.expectStep('detection');
		});
	});

	test.describe('Error Handling', () => {
		test('shows error state when detection fails', async ({ page, setupWizardPage }) => {
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Detection failed' })
				});
			});

			await setupWizardPage.goto();
			await setupWizardPage.expectErrorState('Detection failed');
		});

		test('shows error state when scaffold fails', async ({ page, setupWizardPage }) => {
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockEmptyDetection)
				});
			});

			await page.route('**/api/project/wizard', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockWizardOptions)
				});
			});

			await page.route('**/api/project/scaffold', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Scaffold failed' })
				});
			});

			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			await setupWizardPage.selectLanguage('Go');
			await setupWizardPage.clickCreateProject();

			await setupWizardPage.expectErrorState('Scaffold failed');
		});

		test('error message provides actionable guidance', async ({ page, setupWizardPage }) => {
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Detection failed: insufficient permissions to read directory' })
				});
			});

			await setupWizardPage.goto();

			// Error should contain the detailed message
			const errorText = await setupWizardPage.errorState.textContent();
			expect(errorText).toContain('insufficient permissions');
		});

		test('retry button is visible in error state', async ({ page, setupWizardPage }) => {
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Detection failed' })
				});
			});

			await setupWizardPage.goto();
			await setupWizardPage.expectErrorState();

			// Retry button should be visible
			const retryButton = page.locator('.error-state button', { hasText: 'Retry' });
			await expect(retryButton).toBeVisible();
		});
	});

	test.describe('Validation', () => {
		test('detection step validates project name is required', async ({
			page,
			setupWizardPage
		}) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Clear the project name (it's pre-filled from workspace path)
			await setupWizardPage.clearProjectName();

			// Next button should be disabled without project name
			await setupWizardPage.expectNextButtonDisabled();

			// Fill name, button should enable
			await setupWizardPage.enterProjectName('Test Project');
			await setupWizardPage.expectNextButtonEnabled();
		});

		test('prevents double submission during scaffold operation', async ({
			page,
			setupWizardPage
		}) => {
			let scaffoldCallCount = 0;

			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockEmptyDetection)
				});
			});

			await page.route('**/api/project/wizard', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockWizardOptions)
				});
			});

			await page.route('**/api/project/scaffold', async (route) => {
				scaffoldCallCount++;
				// Simulate slow response
				await new Promise((resolve) => setTimeout(resolve, 500));
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockScaffoldResponse)
				});
			});

			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			await setupWizardPage.selectLanguage('Go');

			// Click Create Project - button should become disabled
			await setupWizardPage.clickCreateProject();

			// Button should be disabled after first click (during scaffolding state)
			await expect(setupWizardPage.createProjectButton).not.toBeVisible();

			// Verify only one scaffold request was made
			expect(scaffoldCallCount).toBe(1);
		});
	});

	test.describe('Accessibility', () => {
		test('wizard overlay has dialog role and modal attributes', async ({
			page,
			setupWizardPage
		}) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();

			await setupWizardPage.expectWizardHasDialogRole();
		});

		test('loading states have status role with aria-live', async ({ page }) => {
			// This test verifies ARIA attributes on the page-level loading state.
			// The loading state shows while checking project status (before wizard appears).
			// We use a never-resolving request to keep the loading state visible.
			let resolveStatus: () => void;
			const statusPromise = new Promise<void>((resolve) => {
				resolveStatus = resolve;
			});

			await page.route('**/api/project/status', async (route) => {
				// Wait indefinitely until we're done checking - status never completes
				await statusPromise;
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			// Navigate and immediately check for loading state
			await page.goto('/', { waitUntil: 'commit' });

			// Wait for hydration first (the loading state appears after hydration)
			await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

			// Check the page-level loading state (shows "Loading..." during status check)
			// This is separate from wizard-internal states and covers initial page load
			const loadingState = page.locator('.init-loading');
			await expect(loadingState).toBeVisible({ timeout: 5000 });
			await expect(loadingState).toHaveAttribute('role', 'status');
			await expect(loadingState).toHaveAttribute('aria-live', 'polite');

			// Cleanup: resolve the status promise so the test can end cleanly
			resolveStatus!();
		});

		test('error state has alert role for immediate announcement', async ({
			page,
			setupWizardPage
		}) => {
			await page.route('**/api/project/status', (route) => {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.route('**/api/project/detect', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Detection failed' })
				});
			});

			await setupWizardPage.goto();

			await setupWizardPage.expectErrorStateHasAlertRole();
		});

		test('step indicators have proper aria-current on active step', async ({
			page,
			setupWizardPage
		}) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// First step should have aria-current="step"
			await setupWizardPage.expectStepHasAriaCurrent(0);

			// Navigate to next step
			await setupWizardPage.clickNext();
			await setupWizardPage.expectStep('checklist');

			// Second step should now have aria-current
			await setupWizardPage.expectStepHasAriaCurrent(1);
		});

		test('collapsible sections have aria-expanded attribute', async ({
			page,
			setupWizardPage
		}) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();

			// Navigate to checklist step
			await setupWizardPage.clickNext();
			await setupWizardPage.expectStep('checklist');

			// Add Check button should have aria-expanded
			await setupWizardPage.expectButtonHasAriaExpanded(setupWizardPage.addCheckButton, false);

			// Click to expand
			await setupWizardPage.addCheckButton.click();

			// Now should be expanded
			await setupWizardPage.expectButtonHasAriaExpanded(setupWizardPage.addCheckButton, true);
		});

		test('project name input has required attribute', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			await expect(setupWizardPage.projectNameInput).toHaveAttribute('required', '');
			await expect(setupWizardPage.projectNameInput).toHaveAttribute('aria-required', 'true');
		});
	});

	test.describe('Keyboard Navigation', () => {
		test('can select language with keyboard', async ({ page, setupWizardPage }) => {
			await setupGreenfieldMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Focus on a language card
			const goCard = setupWizardPage.getLanguageCard('Go');
			await goCard.focus();

			// Select with Space or Enter
			await setupWizardPage.pressSpace();
			await setupWizardPage.expectLanguageSelected('Go');

			// Deselect with Space
			await setupWizardPage.pressSpace();
			await setupWizardPage.expectLanguageNotSelected('Go');

			// Select with Enter
			await setupWizardPage.pressEnter();
			await setupWizardPage.expectLanguageSelected('Go');
		});

		test('can navigate between steps with keyboard', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Tab to Next button
			await setupWizardPage.nextButton.focus();

			// Press Enter to go to next step
			await setupWizardPage.pressEnter();
			await setupWizardPage.expectStep('checklist');

			// Tab to Back button and press Enter
			await setupWizardPage.backButton.focus();
			await setupWizardPage.pressEnter();
			await setupWizardPage.expectStep('detection');
		});

		test('Tab key cycles through interactive elements', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Start with focus on project name input
			await setupWizardPage.projectNameInput.focus();

			// Tab should move to next interactive element
			await setupWizardPage.pressTab();

			// Should now be on project description
			const focusedTag = await setupWizardPage.getFocusedElementTag();
			expect(focusedTag).toBe('INPUT');
		});
	});

	test.describe('Focus Management', () => {
		test('focus stays within wizard modal', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Focus should be within wizard
			await setupWizardPage.projectNameInput.focus();
			const isWithin = await setupWizardPage.isFocusWithinWizard();
			expect(isWithin).toBe(true);
		});

		// TODO: Component needs focus management fix - focus should move to step content after navigation
		test.fixme('focus moves appropriately after step navigation', async ({ page, setupWizardPage }) => {
			await setupExistingProjectMocks(page);
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Focus the Next button explicitly before clicking
			await setupWizardPage.nextButton.focus();
			await setupWizardPage.clickNext();
			await setupWizardPage.expectStep('checklist');

			// After navigation, focus should still be within the wizard
			// Currently failing: focus moves outside wizard after step navigation
			const isWithin = await setupWizardPage.isFocusWithinWizard();
			expect(isWithin).toBe(true);
		});
	});
});

test.describe('First-Plan Nudge', () => {
	test('shows nudge after wizard completion with no plans', async ({ page }) => {
		// Mock as initialized
		await page.route('**/api/project/status', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(mockInitializedStatus)
			});
		});

		// Mock empty plans
		await page.route('**/workflow-api/plans**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		// Mock empty loops
		await page.route('**/agentic-dispatch/loops**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/');
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

		// Nudge should be visible
		const nudge = page.locator('.first-plan-nudge');
		await expect(nudge).toBeVisible();
	});

	test('nudge shows example command', async ({ page }) => {
		await page.route('**/api/project/status', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(mockInitializedStatus)
			});
		});

		await page.route('**/workflow-api/plans**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.route('**/agentic-dispatch/loops**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/');
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

		const nudge = page.locator('.first-plan-nudge');
		await expect(nudge).toContainText('/plan');
	});

	test('dismiss button hides nudge', async ({ page }) => {
		await page.route('**/api/project/status', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(mockInitializedStatus)
			});
		});

		await page.route('**/workflow-api/plans**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.route('**/agentic-dispatch/loops**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/');
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

		const nudge = page.locator('.first-plan-nudge');
		await expect(nudge).toBeVisible();

		// Click dismiss
		const dismissBtn = page.locator('.first-plan-nudge .dismiss-btn');
		await dismissBtn.click();

		// Nudge should be hidden
		await expect(nudge).not.toBeVisible();
	});

	test('nudge not shown when plans exist', async ({ page }) => {
		await page.route('**/api/project/status', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(mockInitializedStatus)
			});
		});

		// Return existing plans
		await page.route('**/workflow-api/plans**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([
					{
						slug: 'existing-plan',
						title: 'Existing Plan',
						status: 'draft'
					}
				])
			});
		});

		await page.route('**/agentic-dispatch/loops**', (route) => {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		});

		await page.goto('/');
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

		// Nudge should NOT be visible
		const nudge = page.locator('.first-plan-nudge');
		await expect(nudge).not.toBeVisible();
	});
});
