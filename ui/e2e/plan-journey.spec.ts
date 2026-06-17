import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, waitForGoal } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton, planListItem, editPlanButton, cascadeStatus } from './helpers/selectors';

/**
 * T1 happy-path plan journey: full lifecycle with mock LLM (hello-world scenario).
 *
 * Two-stage approval:
 *   Round 1: drafted → reviewed (pause) → human clicks "Create Requirements" → approved → cascade
 *   Round 2: scenarios_generated → scenarios_reviewed (pause) → human clicks "Approve & Continue" → ready_for_execution
 *
 * Then: Start Execution → implementing → complete → Done filter
 *
 * Pattern: each test does page.goto() for fresh SSR. Button clicks use
 * waitForResponse to confirm the API call completed before asserting UI state.
 */
test.describe('@t1 @happy-path plan-journey', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('plan-smoke');
		const plan = await createPlan(`Journey test ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, 30000);
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan detail shows Create Requirements button', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.getByRole('button', { name: /Create Requirements/i }).first()).toBeVisible();
	});

	test('first approval triggers cascade to scenarios_reviewed', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Click "Create Requirements" and wait for the promote API response
		const createReqBtn = page.getByRole('button', { name: /Create Requirements/i }).first();
		await Promise.all([
			page.waitForResponse((r) => r.url().includes('/promote') && r.status() === 200),
			createReqBtn.click()
		]);

		// Cascade runs (mock LLM is fast). Wait for UI to show "Approve & Continue",
		// or poll API and reload if SSE missed the events.
		const start = Date.now();
		while (Date.now() - start < 60000) {
			const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
			if (await approveBtn.isVisible().catch(() => false)) break;

			const plan = await getPlan(slug);
			if (plan.stage === 'scenarios_reviewed') {
				await page.reload();
				await waitForHydration(page);
				break;
			}
			await new Promise((r) => setTimeout(r, 1000));
		}

		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 10000 });

		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_reviewed');
	});

	test('editing is available at scenarios_reviewed gate', async ({ page }) => {
		// At this point the plan is at scenarios_reviewed — a human decision gate.
		// The user can edit before approving, so editing should be enabled.
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('scenarios_reviewed');

		// Edit button should be visible at human decision gates.
		await expect(editPlanButton(page).first()).toBeVisible({ timeout: 5000 });
	});

	test('requirements panel shows active requirements', async ({ page }) => {
		// Fresh SSR navigation — load function fetches current data
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const center = page.getByTestId('panel-center');
		await expect(center.getByText('Requirements', { exact: true })).toBeVisible();
		await expect(center.getByText(/\d+ active/)).toBeVisible();
	});

	test('second approval advances to ready_for_execution', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
		await expect(approveBtn).toBeVisible();

		// Click and wait for the promote API response to confirm it succeeded
		const [response] = await Promise.all([
			page.waitForResponse((r) => r.url().includes('/promote') && r.status() === 200),
			approveBtn.click()
		]);
		const body = await response.json();
		console.log(`[journey] Promote response: ${response.status()} stage=${body.stage}`);

		// After promote completes, invalidation re-runs the load function.
		// Wait for the "Start Execution" button to appear.
		await expect(startExecutionButton(page)).toBeVisible({ timeout: 15000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('artifacts view renders backend-written markdown at ready_for_execution', async ({ page }) => {
		// Closes the loop on PR #4: workflow-documents writes
		// .semspec/plans/{slug}/{plan,architecture,requirements,scenarios}.md
		// at every milestone; plan-manager serves them via /artifacts; the
		// viewer fetches + renders them inline. Unit + smoke tests already
		// cover the rendering pipeline against stubs — this one proves the
		// real file → API → DOM path.
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Switch to the Artifacts view mode.
		await page.getByRole('button', { name: 'Artifacts' }).click();

		const view = page.getByTestId('phase-artifacts');
		await expect(view).toBeVisible();

		// plan.md is non-skippable — RenderPlan always emits content, so it
		// must appear regardless of which other phases ran. The TOC chip
		// proves the list endpoint returned it; the section proves the
		// content endpoint + renderer worked end-to-end.
		await expect(page.getByTestId('toc-plan')).toBeVisible({ timeout: 15000 });
		const planSection = page.locator('section#artifact-plan');
		await expect(planSection).toBeVisible();
		await expect(planSection.locator('.markdown-body')).not.toBeEmpty();

		// Hello-world fixture exercises the architecture + requirement +
		// scenario generators, so those artifacts should also be present
		// by ready_for_execution. Asserting them here catches a regression
		// in the workflow-documents milestone watcher.
		for (const name of ['architecture', 'requirements', 'scenarios']) {
			await expect(page.getByTestId(`toc-${name}`)).toBeVisible();
			await expect(page.locator(`section#artifact-${name} .markdown-body`)).not.toBeEmpty();
		}
	});

	test('editing is available at ready_for_execution', async ({ page }) => {
		// After round 2 approval, the plan is at ready_for_execution — a human
		// decision point. Editing should be enabled here so users can review
		// before committing to execution.
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');

		// Edit button should be visible at decision gates.
		await expect(editPlanButton(page).first()).toBeVisible({ timeout: 5000 });
	});

	test('execute plan triggers execution pipeline', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(startExecutionButton(page)).toBeVisible();

		// Click and wait for the execute API response
		await Promise.all([
			page.waitForResponse((r) => r.url().includes('/execute') && r.status() === 202),
			startExecutionButton(page).click()
		]);

		// Poll for stage advancement
		const start = Date.now();
		let plan = await getPlan(slug);
		while (plan.stage === 'ready_for_execution' && Date.now() - start < 30000) {
			await new Promise((r) => setTimeout(r, 1000));
			plan = await getPlan(slug);
		}
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete']).toContain(plan.stage);
	});

	test('execution reaches complete', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(
			page.getByTestId('panel-center').locator('[data-stage="complete"]')
		).toBeVisible({ timeout: 90000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('complete');
	});

	test('completed plan shows in Done filter', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// LeftPanel auto-switches to Feed mode when active loops exist (which
		// is true right after journey execution). In Feed mode the Plans-mode
		// filter chips (All/Active/Drafts/Done) are unmounted. Click Plans
		// mode and wait for the chip group to actually attach before reaching
		// for Done — otherwise the radio query resolves to nothing and times
		// out instead of reflecting "the panel hasn't switched yet".
		const plansRadio = page.getByRole('radio', { name: 'Plans' });
		const doneChip = page.getByRole('radio', { name: 'Done' });
		await expect(async () => {
			if ((await plansRadio.getAttribute('aria-checked')) !== 'true') {
				await plansRadio.click({ timeout: 5000 });
			}
			await expect(doneChip).toBeAttached({ timeout: 5000 });
			await doneChip.click({ timeout: 5000 });
			await expect(doneChip).toHaveAttribute('aria-checked', 'true');
		}).toPass({ timeout: 30000 });
		await expect(planListItem(page, slug)).toBeVisible();
	});

	test('trajectories exist with steps after execution', async ({ page }) => {
		// Loops should exist from the execution phase (tester, builder, reviewer, decomposer)
		const loopsRes = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await loopsRes.json();
		expect(loops.length).toBeGreaterThan(0);
		console.log(`[journey] ${loops.length} loops after execution`);

		// Each loop should have trajectory data with steps
		const loopId = loops[0].loop_id;
		const trajRes = await fetch(`http://localhost:3000/agentic-loop/trajectories/${loopId}`);
		const traj = await trajRes.json();
		expect(traj.steps?.length).toBeGreaterThan(0);
		console.log(`[journey] Loop ${loopId.slice(0, 8)} has ${traj.steps.length} steps`);

		// Verify trajectory detail page renders
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();
		await expect(page.getByTestId('trajectory-id')).toContainText(loopId);
		await expect(page.getByText('Steps', { exact: true })).toBeVisible();
	});
});
