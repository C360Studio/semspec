import {
	test,
	expect,
	seedEmptyProject,
	seedGoProject,
	seedInitializedProject,
	restoreWorkspace,
	waitForWorkspaceSync
} from './helpers/setup';
import { mockUninitializedStatus } from './fixtures/setupWizardData';

/**
 * Setup Wizard E2E Tests
 *
 * These tests use real backend infrastructure where possible.
 * Mocks are only used for:
 * - Error conditions (500 responses)
 * - Timing-dependent scenarios (loading states, double submission)
 * - Specific data requirements (plans list for nudge tests)
 *
 * IMPORTANT: Tests run serially because they modify shared workspace state.
 * Parallel execution would cause tests to interfere with each other.
 */

// File-level serial mode - all tests in this file run sequentially
test.describe.configure({ mode: 'serial' });

// Restore workspace after all tests to leave it in a known state
test.afterAll(async () => {
	await restoreWorkspace();
});

test.describe('Setup Wizard', () => {
	test.describe('Greenfield Flow', () => {
		test.beforeEach(async ({ page }) => {
			// Seed empty project and wait for backend to see it
			await seedEmptyProject();
			await waitForWorkspaceSync(page, true);
		});

		test('shows scaffold step for empty project', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectWizardVisible();
			await setupWizardPage.expectStep('scaffold');
		});

		test('displays available languages from wizard endpoint', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Verify common languages are displayed (from real backend)
			const goCard = setupWizardPage.getLanguageCard('Go');
			await expect(goCard).toBeVisible();

			const pythonCard = setupWizardPage.getLanguageCard('Python');
			await expect(pythonCard).toBeVisible();

			const tsCard = setupWizardPage.getLanguageCard('TypeScript');
			await expect(tsCard).toBeVisible();
		});

		test('selecting language shows available frameworks', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Initially no frameworks visible (no language selected)
			await setupWizardPage.expectFrameworkHidden('Flask');

			// Select Python
			await setupWizardPage.selectLanguage('Python');
			await setupWizardPage.expectLanguageSelected('Python');

			// Python frameworks should now be visible
			await setupWizardPage.expectFrameworkVisible('Flask');
		});

		test('deselecting language removes dependent frameworks', async ({ setupWizardPage }) => {
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

		test('Create Project button disabled without language selection', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Button should be disabled initially
			await setupWizardPage.expectCreateProjectButtonDisabled();

			// Select a language
			await setupWizardPage.selectLanguage('Go');

			// Button should now be enabled
			await setupWizardPage.expectCreateProjectButtonEnabled();
		});

		test('scaffold creates marker files and proceeds to detection', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('scaffold');

			// Select Go and create project
			await setupWizardPage.selectLanguage('Go');
			await setupWizardPage.clickCreateProject();

			// Should show scaffolding state briefly, then proceed to detection
			await setupWizardPage.expectStep('detection');

			// Real backend detected Go from the scaffolded files
			await setupWizardPage.expectDetectedLanguage('Go');
		});
	});

	test.describe('Existing Project Flow', () => {
		test.beforeEach(async ({ page }) => {
			// Seed Go project and wait for backend to see it
			await seedGoProject();
			await waitForWorkspaceSync(page, false);
		});

		test('skips scaffold for existing project', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectWizardVisible();

			// Should go directly to detection step, not scaffold
			await setupWizardPage.expectStep('detection');
		});

		test('back button disabled on detection step for non-greenfield', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Back button should be disabled since there's no scaffold step to go back to
			await setupWizardPage.expectBackButtonDisabled();
		});

		test('displays detected languages and frameworks', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Verify detected items are shown (real detection from go.mod)
			await setupWizardPage.expectDetectedLanguage('Go');
		});
	});

	test.describe('Completion Step', () => {
		test('shows activity view when project is initialized', async ({ setupWizardPage }) => {
			await seedInitializedProject();
			await setupWizardPage.goto();

			// Should NOT show wizard since project is initialized
			await setupWizardPage.expectWizardHidden();
		});
	});

	test.describe('Navigation', () => {
		test.beforeEach(async ({ page }) => {
			await seedGoProject();
			await waitForWorkspaceSync(page, false);
		});

		test('can navigate through all wizard steps', async ({ setupWizardPage }) => {
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

		test('step indicators show correct count', async ({ setupWizardPage }) => {
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// For existing project: detection, checklist, standards = 3 steps
			await setupWizardPage.expectStepCount(3);
		});

		test('can navigate back through steps', async ({ setupWizardPage }) => {
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
		// Error tests require mocks to simulate 500 responses
		test('shows error state when detection fails', async ({ page, setupWizardPage }) => {
			await seedGoProject();

			// Mock detect to return error
			await page.route('**/project-api/detect', (route) => {
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
			await seedEmptyProject();
			await waitForWorkspaceSync(page, true);

			// Mock scaffold to return error
			await page.route('**/project-api/scaffold', (route) => {
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
			await seedGoProject();

			await page.route('**/project-api/detect', (route) => {
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
			await seedGoProject();

			await page.route('**/project-api/detect', (route) => {
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
		test('detection step validates project name is required', async ({ setupWizardPage }) => {
			await seedGoProject();
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

		// This test requires a delay mock for timing control
		test('prevents double submission during scaffold operation', async ({ page, setupWizardPage }) => {
			await seedEmptyProject();
			await waitForWorkspaceSync(page, true);

			let scaffoldCallCount = 0;

			// Mock scaffold with delay to test double-click prevention
			await page.route('**/project-api/scaffold', async (route) => {
				scaffoldCallCount++;
				// Simulate slow response
				await new Promise((resolve) => setTimeout(resolve, 500));
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ files_created: ['go.mod'], semspec_dir: '.semspec' })
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
		test('wizard overlay has dialog role and modal attributes', async ({ setupWizardPage }) => {
			await seedGoProject();
			await setupWizardPage.goto();

			await setupWizardPage.expectWizardHasDialogRole();
		});

		// This test requires a never-resolving mock to keep loading state visible
		test('loading states have status role with aria-live', async ({ page }) => {
			await seedGoProject();

			// Use never-resolving request to keep loading state visible
			let resolveStatus: () => void;
			const statusPromise = new Promise<void>((resolve) => {
				resolveStatus = resolve;
			});

			await page.route('**/project-api/status', async (route) => {
				await statusPromise;
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(mockUninitializedStatus)
				});
			});

			await page.goto('/', { waitUntil: 'commit' });
			await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

			const loadingState = page.locator('.init-loading');
			await expect(loadingState).toBeVisible({ timeout: 5000 });
			await expect(loadingState).toHaveAttribute('role', 'status');
			await expect(loadingState).toHaveAttribute('aria-live', 'polite');

			resolveStatus!();
		});

		test('error state has alert role for immediate announcement', async ({ page, setupWizardPage }) => {
			await seedGoProject();

			await page.route('**/project-api/detect', (route) => {
				return route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Detection failed' })
				});
			});

			await setupWizardPage.goto();

			await setupWizardPage.expectErrorStateHasAlertRole();
		});

		test('step indicators have proper aria-current on active step', async ({ setupWizardPage }) => {
			await seedGoProject();
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

		test('collapsible sections have aria-expanded attribute', async ({ setupWizardPage }) => {
			await seedGoProject();
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

		test('project name input has required attribute', async ({ setupWizardPage }) => {
			await seedGoProject();
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			await expect(setupWizardPage.projectNameInput).toHaveAttribute('required', '');
			await expect(setupWizardPage.projectNameInput).toHaveAttribute('aria-required', 'true');
		});
	});

	test.describe('Keyboard Navigation', () => {
		test('can select language with keyboard', async ({ page, setupWizardPage }) => {
			await seedEmptyProject();
			await waitForWorkspaceSync(page, true);
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

		test('can navigate between steps with keyboard', async ({ setupWizardPage }) => {
			await seedGoProject();
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

		test('Tab key cycles through interactive elements', async ({ setupWizardPage }) => {
			await seedGoProject();
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
		test('focus stays within wizard modal', async ({ setupWizardPage }) => {
			await seedGoProject();
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			// Focus should be within wizard
			await setupWizardPage.projectNameInput.focus();
			const isWithin = await setupWizardPage.isFocusWithinWizard();
			expect(isWithin).toBe(true);
		});

		// TODO: Component needs focus management fix - focus should move to step content after navigation
		test.fixme('focus moves appropriately after step navigation', async ({ setupWizardPage }) => {
			await seedGoProject();
			await setupWizardPage.goto();
			await setupWizardPage.expectStep('detection');

			await setupWizardPage.nextButton.focus();
			await setupWizardPage.clickNext();
			await setupWizardPage.expectStep('checklist');

			const isWithin = await setupWizardPage.isFocusWithinWizard();
			expect(isWithin).toBe(true);
		});
	});
});

test.describe('First-Plan Nudge', () => {
	test('shows nudge after wizard completion with no plans', async ({ page }) => {
		await seedInitializedProject();

		// Mock empty plans (need specific empty array)
		await page.route('**/workflow-api/plans**', (route) => {
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
		await seedInitializedProject();

		await page.route('**/workflow-api/plans**', (route) => {
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
		await seedInitializedProject();

		await page.route('**/workflow-api/plans**', (route) => {
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
		await seedInitializedProject();

		// Mock existing plans (need specific data)
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

		await page.goto('/');
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 15000 });

		// Nudge should NOT be visible
		const nudge = page.locator('.first-plan-nudge');
		await expect(nudge).not.toBeVisible();
	});
});
