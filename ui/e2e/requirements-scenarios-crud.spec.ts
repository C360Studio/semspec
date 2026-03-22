import { test, expect, mockPlan, waitForHydration } from './helpers/setup';
import type { Requirement } from '../src/lib/types/requirement';
import type { Scenario } from '../src/lib/types/scenario';

/**
 * Tests for requirement and scenario CRUD operations in the plan detail UI.
 *
 * Coverage:
 * - RequirementPanel: add form, validation, error state
 * - RequirementDetail: inline edit (title + description), deprecate, delete
 * - RequirementDetail: add scenario BDD form, delete scenario, validation
 *
 * All tests run against mock API routes — no real backend needed.
 * The plan is always set to stage "approved" so canEdit is true and
 * CRUD controls are visible.
 */

// ============================================================================
// Mock data builders
// ============================================================================

const PLAN_SLUG = 'test-crud';

function makeRequirement(overrides: Partial<Requirement> & { id: string; title: string }): Requirement {
	return {
		plan_id: `plan-${PLAN_SLUG}`,
		description: '',
		status: 'active',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		...overrides,
	};
}

function makeScenario(
	overrides: Partial<Scenario> & { id: string; requirement_id: string }
): Scenario {
	return {
		given: 'a user is authenticated',
		when: 'they perform an action',
		then: ['the expected outcome occurs'],
		status: 'pending',
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		...overrides,
	};
}

// ============================================================================
// Route setup helpers
// ============================================================================

/**
 * Register baseline routes used by every plan detail page load.
 * Individual tests add mutation interception on top of these.
 */
async function setupPlanRoutes(
	page: import('@playwright/test').Page,
	requirements: Requirement[] = [],
	scenarios: Scenario[] = []
): Promise<void> {
	const plan = mockPlan({
		slug: PLAN_SLUG,
		title: 'CRUD Test Plan',
		goal: 'Test requirement and scenario CRUD',
		approved: true,
		stage: 'approved',
	});

	await page.route('**/workflow-api/plans', (route) => {
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([plan]),
		});
	});

	await page.route(`**/workflow-api/plans/${PLAN_SLUG}`, (route) => {
		if (route.request().method() === 'GET') {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(plan),
			});
		} else {
			route.continue();
		}
	});

	// Phase/task endpoints return empty arrays for this test plan.
	await page.route(`**/workflow-api/plans/${PLAN_SLUG}/phases`, (route) => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});

	await page.route(`**/workflow-api/plans/${PLAN_SLUG}/tasks`, (route) => {
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
	});

	// Requirements list — can be updated per-test before navigation.
	await page.route(`**/workflow-api/plans/${PLAN_SLUG}/requirements`, (route) => {
		if (route.request().method() === 'GET') {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(requirements),
			});
		} else {
			route.continue();
		}
	});

	// Scenarios list (query-string variants all handled by the wildcard).
	await page.route(`**/workflow-api/plans/${PLAN_SLUG}/scenarios**`, (route) => {
		if (route.request().method() === 'GET') {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(scenarios),
			});
		} else {
			route.continue();
		}
	});
}

// ============================================================================
// Requirement CRUD tests
// ============================================================================

test.describe('Requirement CRUD', () => {
	test.describe('RequirementPanel — Add Requirement', () => {
		test('opens add form when Add button is clicked', async ({ page, planDetailPage }) => {
			await setupPlanRoutes(page);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			// The RequirementPanel renders inside PlanDetail when no requirement is selected.
			// Click the "Add" button in the panel header.
			const addBtn = page.locator('.requirement-panel .btn-ghost', { hasText: 'Add' });
			await expect(addBtn).toBeVisible();
			await addBtn.click();

			// Add form should appear with title input.
			await expect(page.locator('#req-title')).toBeVisible();
			await expect(page.locator('#req-description')).toBeVisible();
			await expect(page.locator('.add-form button', { hasText: 'Add Requirement' })).toBeVisible();
		});

		test('Add Requirement button is disabled when title is empty', async ({
			page,
			planDetailPage,
		}) => {
			await setupPlanRoutes(page);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			const addBtn = page.locator('.requirement-panel .btn-ghost', { hasText: 'Add' });
			await addBtn.click();

			// The submit button must be disabled when the title input is empty.
			const submitBtn = page.locator('.add-form .btn-primary', { hasText: 'Add Requirement' });
			await expect(submitBtn).toBeDisabled();
		});

		test('submits POST with title and displays new requirement in list', async ({
			page,
			planDetailPage,
		}) => {
			let capturedBody: Record<string, unknown> | null = null;
			const currentRequirements: Requirement[] = [];

			await setupPlanRoutes(page, currentRequirements);

			// Intercept POST /requirements to verify the payload and return the created object.
			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/requirements`, async (route) => {
				if (route.request().method() === 'POST') {
					capturedBody = route.request().postDataJSON() as Record<string, unknown>;
					const newReq = makeRequirement({
						id: 'req-new',
						title: capturedBody.title as string,
						description: (capturedBody.description as string) ?? '',
					});
					// Add to in-memory list so the GET re-fetch returns it.
					currentRequirements.push(newReq);
					await route.fulfill({
						status: 201,
						contentType: 'application/json',
						body: JSON.stringify(newReq),
					});
				} else if (route.request().method() === 'GET') {
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(currentRequirements),
					});
				} else {
					await route.continue();
				}
			});

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			// Open the add form.
			const addBtn = page.locator('.requirement-panel .btn-ghost', { hasText: 'Add' });
			await addBtn.click();

			// Type into the title field using pressSequentially so Svelte reactive
			// bind:value fires real keystroke events.
			const titleInput = page.locator('#req-title');
			await titleInput.pressSequentially('User can reset their password');

			// Verify the submit button is now enabled.
			const submitBtn = page.locator('.add-form .btn-primary', { hasText: 'Add Requirement' });
			await expect(submitBtn).toBeEnabled();

			await submitBtn.click();

			// Wait for the new requirement to appear in the requirement list.
			const reqItem = page.locator('.requirement-item', {
				hasText: 'User can reset their password',
			});
			await expect(reqItem).toBeVisible({ timeout: 5000 });

			// Verify the captured POST body had the correct title.
			expect(capturedBody).not.toBeNull();
			expect(capturedBody!['title']).toBe('User can reset their password');
		});

		test('shows submit error message when POST returns 500', async ({
			page,
			planDetailPage,
		}) => {
			await setupPlanRoutes(page);

			// Override POST to simulate a server error.
			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/requirements`, async (route) => {
				if (route.request().method() === 'POST') {
					await route.fulfill({
						status: 500,
						contentType: 'application/json',
						body: JSON.stringify({ error: 'Internal Server Error' }),
					});
				} else {
					await route.continue();
				}
			});

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			const addBtn = page.locator('.requirement-panel .btn-ghost', { hasText: 'Add' });
			await addBtn.click();

			const titleInput = page.locator('#req-title');
			await titleInput.pressSequentially('A failing requirement');

			const submitBtn = page.locator('.add-form .btn-primary', { hasText: 'Add Requirement' });
			await submitBtn.click();

			// The component renders submitError in .form-error when the API call fails.
			const errorEl = page.locator('.add-form .form-error[role="alert"]');
			await expect(errorEl).toBeVisible({ timeout: 5000 });
		});

		test('deprecates a requirement and status badge changes', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-active', title: 'Active Requirement' });
			const deprecatedReq = { ...req, status: 'deprecated' as const };

			await setupPlanRoutes(page, [req]);

			let deprecateCalled = false;
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/requirements/${req.id}/deprecate`,
				async (route) => {
					deprecateCalled = true;
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(deprecatedReq),
					});
				}
			);

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			// The deprecate button (.deprecate-btn) sits inside .req-header and is opacity:0
			// until hover. Use force:true to click it without hovering.
			const reqItem = page.locator('.requirement-item', { hasText: 'Active Requirement' });
			await expect(reqItem).toBeVisible();

			const deprecateBtn = reqItem.locator('.deprecate-btn');
			await deprecateBtn.click({ force: true });

			// After deprecation the API returns the updated object; the component re-renders
			// the status badge. Wait for the "Deprecated" badge to appear.
			const deprecatedBadge = reqItem.locator('.req-status-badge.badge-neutral', {
				hasText: 'Deprecated',
			});
			await expect(deprecatedBadge).toBeVisible({ timeout: 5000 });
			expect(deprecateCalled).toBe(true);
		});
	});

	test.describe('RequirementDetail — Edit Requirement', () => {
		test('shows Edit, Deprecate and Delete buttons for active requirement', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({
				id: 'req-editable',
				title: 'Editable Requirement',
				description: 'Some description',
			});

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			// Click the requirement node in the nav tree to open RequirementDetail.
			await planDetailPage.selectRequirementInTree('Editable Requirement');

			const detailHeader = page.locator('.requirement-detail .header-actions');
			await expect(detailHeader.locator('button', { hasText: 'Edit' })).toBeVisible();
			// Deprecate and Delete are icon-only ghost buttons with title attributes.
			await expect(detailHeader.locator('button[title="Deprecate"]')).toBeVisible();
			await expect(detailHeader.locator('button[title="Delete"]')).toBeVisible();
		});

		test('entering edit mode shows title input with current value', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({
				id: 'req-edit-title',
				title: 'Original Title',
				description: 'Original description',
			});

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Original Title');

			const editBtn = page.locator('.requirement-detail .header-actions button', {
				hasText: 'Edit',
			});
			await editBtn.click();

			// Title input should appear pre-filled with the current title.
			const titleInput = page.locator('.requirement-detail .title-input');
			await expect(titleInput).toBeVisible();
			await expect(titleInput).toHaveValue('Original Title');

			// Description textarea should appear pre-filled.
			const descTextarea = page.locator('#edit-desc');
			await expect(descTextarea).toBeVisible();
			await expect(descTextarea).toHaveValue('Original description');
		});

		test('saves updated title via PATCH and reflects change in detail header', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({
				id: 'req-save-title',
				title: 'Old Title',
				description: '',
			});
			const updatedReq = { ...req, title: 'New Title' };

			// Maintain a mutable copy so the GET re-fetch after save returns updated data.
			let currentReq = req;

			await setupPlanRoutes(page, [req]);

			let patchCalled = false;
			let patchBody: Record<string, unknown> | null = null;

			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/requirements/${req.id}`,
				async (route) => {
					if (route.request().method() === 'PATCH') {
						patchCalled = true;
						patchBody = route.request().postDataJSON() as Record<string, unknown>;
						currentReq = updatedReq;
						await route.fulfill({
							status: 200,
							contentType: 'application/json',
							body: JSON.stringify(updatedReq),
						});
					} else if (route.request().method() === 'GET') {
						await route.fulfill({
							status: 200,
							contentType: 'application/json',
							body: JSON.stringify(currentReq),
						});
					} else {
						await route.continue();
					}
				}
			);

			// Override the list GET to return updated list after save.
			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/requirements`, async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([currentReq]),
				});
			});

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Old Title');

			const editBtn = page.locator('.requirement-detail .header-actions button', {
				hasText: 'Edit',
			});
			await editBtn.click();

			// Clear existing title and type new one.
			const titleInput = page.locator('.requirement-detail .title-input');
			await titleInput.clear();
			await titleInput.pressSequentially('New Title');

			const saveBtn = page.locator('.requirement-detail .edit-actions .btn-primary', {
				hasText: 'Save',
			});
			await expect(saveBtn).toBeEnabled();
			await saveBtn.click();

			// After save the component exits edit mode; the updated title should be in the header.
			await expect(page.locator('.requirement-detail .detail-title')).toContainText('New Title', {
				timeout: 5000,
			});

			expect(patchCalled).toBe(true);
			expect(patchBody!['title']).toBe('New Title');
		});

		test('Save button is disabled when title is cleared', async ({ page, planDetailPage }) => {
			const req = makeRequirement({ id: 'req-no-title', title: 'Has a Title' });

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Has a Title');

			const editBtn = page.locator('.requirement-detail .header-actions button', {
				hasText: 'Edit',
			});
			await editBtn.click();

			const titleInput = page.locator('.requirement-detail .title-input');
			await titleInput.clear();

			const saveBtn = page.locator('.requirement-detail .edit-actions .btn-primary', {
				hasText: 'Save',
			});
			await expect(saveBtn).toBeDisabled();
		});

		test('cancel exits edit mode without saving', async ({ page, planDetailPage }) => {
			const req = makeRequirement({ id: 'req-cancel', title: 'Cancellable Requirement' });

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Cancellable Requirement');

			const editBtn = page.locator('.requirement-detail .header-actions button', {
				hasText: 'Edit',
			});
			await editBtn.click();

			const titleInput = page.locator('.requirement-detail .title-input');
			await titleInput.clear();
			await titleInput.pressSequentially('Changed But Not Saved');

			const cancelBtn = page.locator('.requirement-detail .edit-actions .btn-ghost', {
				hasText: 'Cancel',
			});
			await cancelBtn.click();

			// Edit mode should exit; original title should still show.
			await expect(page.locator('.requirement-detail .detail-title')).toContainText(
				'Cancellable Requirement'
			);
			await expect(titleInput).not.toBeVisible();
		});

		test('deprecates requirement from RequirementDetail and calls API', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-dep', title: 'Req to Deprecate' });
			const deprecatedReq = { ...req, status: 'deprecated' as const };

			let currentList = [req];
			await setupPlanRoutes(page, currentList);

			let deprecateCalled = false;
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/requirements/${req.id}/deprecate`,
				async (route) => {
					deprecateCalled = true;
					currentList = [deprecatedReq];
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(deprecatedReq),
					});
				}
			);

			// Return updated list on re-fetch.
			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/requirements`, async (route) => {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(currentList),
				});
			});

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Req to Deprecate');

			// Click the archive icon button (title="Deprecate") in RequirementDetail header.
			const deprecateBtn = page.locator(
				'.requirement-detail .header-actions button[title="Deprecate"]'
			);
			await deprecateBtn.click();

			expect(deprecateCalled).toBe(true);
		});
	});
});

// ============================================================================
// Scenario CRUD tests
// ============================================================================

test.describe('Scenario CRUD', () => {
	test.describe('RequirementDetail — Add Scenario Form', () => {
		test('opens add scenario form when Add button is clicked', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-sc-add', title: 'Req with Scenarios' });

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Req with Scenarios');

			// Click the "Add" button in the Scenarios section header.
			const addScenarioBtn = page.locator('.scenarios-section .btn-ghost', { hasText: 'Add' });
			await expect(addScenarioBtn).toBeVisible();
			await addScenarioBtn.click();

			// BDD form fields should appear.
			await expect(page.locator('#sc-given')).toBeVisible();
			await expect(page.locator('#sc-when')).toBeVisible();
			await expect(page.locator('#sc-then')).toBeVisible();
		});

		test('Add Scenario button is disabled when any BDD field is empty', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-sc-validate', title: 'BDD Validation Req' });

			await setupPlanRoutes(page, [req]);
			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('BDD Validation Req');

			const addScenarioBtn = page.locator('.scenarios-section .btn-ghost', { hasText: 'Add' });
			await addScenarioBtn.click();

			const submitBtn = page.locator('.add-scenario-form .btn-primary', {
				hasText: 'Add Scenario',
			});

			// No fields filled — button is disabled.
			await expect(submitBtn).toBeDisabled();

			// Fill Given only — still disabled.
			await page.locator('#sc-given').pressSequentially('a logged-in user');
			await expect(submitBtn).toBeDisabled();

			// Fill When — still disabled (Then is empty).
			await page.locator('#sc-when').pressSequentially('they view the dashboard');
			await expect(submitBtn).toBeDisabled();

			// Fill Then — now enabled.
			await page.locator('#sc-then').pressSequentially('the dashboard renders');
			await expect(submitBtn).toBeEnabled();
		});

		test('submits POST with BDD fields and scenario appears in list', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-sc-post', title: 'Scenario POST Req' });
			const currentScenarios: Scenario[] = [];
			let capturedBody: Record<string, unknown> | null = null;

			await setupPlanRoutes(page, [req], currentScenarios);

			// Intercept POST /scenarios and return the newly created scenario.
			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/scenarios`, async (route) => {
				if (route.request().method() === 'POST') {
					capturedBody = route.request().postDataJSON() as Record<string, unknown>;
					const newScenario = makeScenario({
						id: 'sc-new',
						requirement_id: req.id,
						given: capturedBody['given'] as string,
						when: capturedBody['when'] as string,
						then: capturedBody['then'] as string[],
					});
					currentScenarios.push(newScenario);
					await route.fulfill({
						status: 201,
						contentType: 'application/json',
						body: JSON.stringify(newScenario),
					});
				} else if (route.request().method() === 'GET') {
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify(currentScenarios),
					});
				} else {
					await route.continue();
				}
			});

			// Also override scenarios wildcard for re-fetch after add.
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios**`,
				async (route) => {
					if (route.request().method() === 'GET') {
						await route.fulfill({
							status: 200,
							contentType: 'application/json',
							body: JSON.stringify(currentScenarios),
						});
					} else {
						await route.continue();
					}
				}
			);

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Scenario POST Req');

			const addScenarioBtn = page.locator('.scenarios-section .btn-ghost', { hasText: 'Add' });
			await addScenarioBtn.click();

			await page.locator('#sc-given').pressSequentially('the system is online');
			await page.locator('#sc-when').pressSequentially('a health check is requested');
			// The "Then" textarea accepts newline-separated outcomes.
			await page.locator('#sc-then').pressSequentially('the status is 200');

			const submitBtn = page.locator('.add-scenario-form .btn-primary', {
				hasText: 'Add Scenario',
			});
			await submitBtn.click();

			// Wait for the scenario detail card to appear in the scenarios list.
			const scenarioCard = page.locator('.scenario-detail', {
				hasText: 'the system is online',
			});
			await expect(scenarioCard).toBeVisible({ timeout: 5000 });

			// Verify request body fields.
			expect(capturedBody).not.toBeNull();
			expect(capturedBody!['given']).toBe('the system is online');
			expect(capturedBody!['when']).toBe('a health check is requested');
			expect(capturedBody!['requirement_id']).toBe(req.id);
		});

		test('submitting with multiline Then sends array of outcomes', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-sc-multiline', title: 'Multi-Then Req' });
			let capturedBody: Record<string, unknown> | null = null;

			await setupPlanRoutes(page, [req]);

			await page.route(`**/workflow-api/plans/${PLAN_SLUG}/scenarios`, async (route) => {
				if (route.request().method() === 'POST') {
					capturedBody = route.request().postDataJSON() as Record<string, unknown>;
					const newScenario = makeScenario({
						id: 'sc-multi',
						requirement_id: req.id,
						given: capturedBody['given'] as string,
						when: capturedBody['when'] as string,
						then: capturedBody['then'] as string[],
					});
					await route.fulfill({
						status: 201,
						contentType: 'application/json',
						body: JSON.stringify(newScenario),
					});
				} else {
					await route.continue();
				}
			});

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Multi-Then Req');

			const addScenarioBtn = page.locator('.scenarios-section .btn-ghost', { hasText: 'Add' });
			await addScenarioBtn.click();

			await page.locator('#sc-given').pressSequentially('a user with admin role');
			await page.locator('#sc-when').pressSequentially('they access the settings page');

			// Type two Then outcomes separated by newline.
			// The component splits newlines: `newThen.trim().split('\n').filter(Boolean)`
			const thenTextarea = page.locator('#sc-then');
			await thenTextarea.pressSequentially('the page loads');
			await thenTextarea.press('Enter');
			await thenTextarea.pressSequentially('admin controls are visible');

			const submitBtn = page.locator('.add-scenario-form .btn-primary', {
				hasText: 'Add Scenario',
			});
			await submitBtn.click();

			// The POST body "then" field must be an array with two items.
			await expect
				.poll(() => capturedBody, { timeout: 5000 })
				.not.toBeNull();
			const thenArr = capturedBody!['then'] as string[];
			expect(Array.isArray(thenArr)).toBe(true);
			expect(thenArr.length).toBe(2);
			expect(thenArr[0]).toBe('the page loads');
			expect(thenArr[1]).toBe('admin controls are visible');
		});
	});

	test.describe('RequirementDetail — Delete Scenario', () => {
		test('delete button appears on hover and removes scenario after DELETE', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-sc-del', title: 'Delete Scenario Req' });
			const scenario = makeScenario({
				id: 'sc-to-delete',
				requirement_id: req.id,
				given: 'a scenario exists',
				when: 'the user deletes it',
				then: ['it is removed'],
			});

			let currentScenarios = [scenario];
			await setupPlanRoutes(page, [req], currentScenarios);

			// Override per-requirement scenario fetch used by RequirementDetail.
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios**`,
				async (route) => {
					if (route.request().method() === 'GET') {
						await route.fulfill({
							status: 200,
							contentType: 'application/json',
							body: JSON.stringify(currentScenarios),
						});
					} else {
						await route.continue();
					}
				}
			);

			let deleteCalled = false;
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios/${scenario.id}`,
				async (route) => {
					if (route.request().method() === 'DELETE') {
						deleteCalled = true;
						currentScenarios = [];
						await route.fulfill({ status: 204, body: '' });
					} else {
						await route.continue();
					}
				}
			);

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			await planDetailPage.selectRequirementInTree('Delete Scenario Req');

			// The scenario card should be visible.
			const scenarioItem = page.locator('.scenario-item', {
				hasText: 'a scenario exists',
			});
			await expect(scenarioItem).toBeVisible({ timeout: 5000 });

			// The delete button is opacity:0 and only shows on hover.
			// Use force:true to click it directly.
			const deleteBtn = scenarioItem.locator('.delete-scenario-btn');
			await deleteBtn.click({ force: true });

			// Wait for the scenario to disappear from the DOM after the re-fetch.
			await expect(scenarioItem).not.toBeVisible({ timeout: 5000 });
			expect(deleteCalled).toBe(true);
		});
	});

	test.describe('RequirementPanel — Inline Scenario Expansion', () => {
		test('expanding a requirement in RequirementPanel fetches and displays its scenarios', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-expand-sc', title: 'Expandable Req' });
			const scenario = makeScenario({
				id: 'sc-expand-1',
				requirement_id: req.id,
				given: 'a condition holds',
				when: 'an event fires',
				then: ['the result is observable'],
			});

			await setupPlanRoutes(page, [req]);

			// The RequirementPanel fetches scenarios by requirement ID when expanded.
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios?requirement_id=${req.id}`,
				async (route) => {
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([scenario]),
					});
				}
			);

			// Also handle the wildcard so any scenario request is satisfied.
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios**`,
				async (route) => {
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([scenario]),
					});
				}
			);

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			// RequirementPanel is visible when PlanDetail is shown (no selection).
			const reqItem = page.locator('.requirement-item', { hasText: 'Expandable Req' });
			await expect(reqItem).toBeVisible();

			// Click the expand chevron button.
			const expandBtn = reqItem.locator('.expand-btn');
			await expandBtn.click();

			// After expansion the scenario card should appear inside .scenarios-container.
			const scenarioCard = reqItem.locator('.scenario-detail', {
				hasText: 'a condition holds',
			});
			await expect(scenarioCard).toBeVisible({ timeout: 5000 });
		});

		test('shows empty message when requirement has no scenarios', async ({
			page,
			planDetailPage,
		}) => {
			const req = makeRequirement({ id: 'req-no-sc', title: 'No Scenarios Req' });

			await setupPlanRoutes(page, [req]);

			// Scenarios endpoint returns empty.
			await page.route(
				`**/workflow-api/plans/${PLAN_SLUG}/scenarios**`,
				async (route) => {
					await route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify([]),
					});
				}
			);

			await planDetailPage.goto(PLAN_SLUG);
			await waitForHydration(page);

			const reqItem = page.locator('.requirement-item', { hasText: 'No Scenarios Req' });
			const expandBtn = reqItem.locator('.expand-btn');
			await expandBtn.click();

			const emptyMsg = reqItem.locator('.no-scenarios');
			await expect(emptyMsg).toBeVisible({ timeout: 5000 });
			await expect(emptyMsg).toContainText('No scenarios linked to this requirement');
		});
	});
});
