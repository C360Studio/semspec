import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';

/**
 * UX gaps caught 2026-05-18 during local-LLM demo session:
 *
 *   1. While the plan was in `drafting` (planner LLM running) and
 *      `reviewing_draft` (plan-reviewer LLM running), the UI showed
 *      "No plan details yet" + an ENABLED "Create Requirements" button.
 *      Reads as wedged + invites premature promotion. Fix: guidance.ts
 *      now returns isLoading hint + hides approve button for these stages.
 *
 *   2. After plan-reviewer R1 finished, the "Reviews" section said
 *      "No review results available" even though `plan.review_verdict`
 *      and `plan.review_summary` were both populated on the plan API.
 *      The ReviewDashboard component only reads code-review aggregation
 *      results (post-implementation). Fix: new PlanReviewCard renders
 *      the R1 verdict + summary inline in the Reviews section.
 *
 * Both bugs were invisible to the existing mock-LLM journey tests because
 * mock fixtures respond in <1ms — the entire drafting+reviewing window
 * collapses to milliseconds, so the UI never paints the in-progress state
 * for any test to see. This spec uses the mock LLM's `delay_ms` knob to
 * stretch that window open.
 */
test.describe('@t1 @ux plan-pre-execution-ux', () => {
	const mockLLM = new MockLLMClient();

	test.afterEach(async () => {
		// Always clear the delay so a later test doesn't inherit a slow mock.
		await mockLLM.setDelay(0).catch(() => {});
	});

	test('drafting state hides Create Requirements + shows loading hint', async ({ page }) => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('hello-world');

		// 6s delay per mock response. Long enough to navigate + hydrate + assert
		// while the planner's first call is still in flight, short enough that the
		// after-block doesn't block other tests excessively if we forget setDelay(0).
		await mockLLM.setDelay(6000);

		const plan = await createPlan(`Drafting UX ${Date.now()}`);

		try {
			await page.goto(`/plans/${plan.slug}`);
			await waitForHydration(page);

			// PRE-CHECK: confirm the plan is still pre-draft (planner LLM hasn't
			// returned yet). If the delay was too short, the plan will already
			// have a goal and this assertion catches the test-data drift rather
			// than mis-diagnosing the UI.
			const live = await getPlan(plan.slug);
			expect(['created', 'drafting', 'drafted', 'reviewing_draft']).toContain(live.stage);

			// The new guidance.ts branches set `isLoading=true` and hide the
			// approve button while the planner or plan-reviewer LLM is running.
			// Match the message text loosely so future copy tweaks don't break
			// the test as long as the intent is preserved.
			// Scoped to `.guidance-hint` only — `[role="status"]` is too broad and
			// matches the board mode-indicator badge ("◐ Draft") which renders
			// above the plan detail panel.
			const guidanceHint = page.locator('.guidance-hint');
			await expect(guidanceHint).toContainText(/composing|drafted|reviewer|reviewing/i, {
				timeout: 5000
			});

			// "Create Requirements" must NOT be available — clicking it now would
			// promote the plan before it's been drafted/reviewed.
			await expect(page.getByRole('button', { name: /Create Requirements/i })).toHaveCount(0);
		} finally {
			await mockLLM.setDelay(0).catch(() => {});
			await deletePlan(plan.slug).catch(() => {});
		}
	});

	test('R1 plan-reviewer verdict + summary surface in the Reviews section', async ({ page }) => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('hello-world');

		// Slow each LLM call so the plan doesn't race past `reviewed` before we
		// can scroll + click + assert. Without this the mock journey reaches
		// `implementing` in ~100ms and a `$effect` in +page.svelte resets the
		// user's expand-toggle when stage enters EXECUTING_STAGES, collapsing
		// the section just as the test tries to read PlanReviewCard.
		await mockLLM.setDelay(3000);

		const plan = await createPlan(`R1 verdict UX ${Date.now()}`);

		try {
			// Wait until plan-reviewer has produced a verdict on the plan object.
			// With 3s mock delay, planner+reviewer each take ~3s; 30s is comfortable
			// margin while still keeping the test bounded.
			const deadline = Date.now() + 30_000;
			let reviewed = await getPlan(plan.slug);
			while (Date.now() < deadline && !reviewed.review_verdict) {
				await new Promise((r) => setTimeout(r, 250));
				reviewed = await getPlan(plan.slug);
			}
			expect(reviewed.review_verdict, 'plan-reviewer should produce a verdict via mock fixtures').toBeTruthy();
			expect(reviewed.review_summary, 'plan-reviewer should produce a summary via mock fixtures').toBeTruthy();

			await page.goto(`/plans/${plan.slug}`);
			await waitForHydration(page);

			// The Reviews section is collapsible AND lives below the fold for a
			// `reviewed`-stage plan. The plan-manager is configured with
			// auto_approve=false so stage holds at `reviewed` indefinitely —
			// stable for assertion. The activity feed (50+ events accumulated
			// across the test session) re-renders frequently, which makes
			// Playwright's regular click() block on stability checks. Dispatch
			// the click event programmatically — Svelte's onclick handler reads
			// from the bubbled event the same way regardless of source.
			const reviewToggle = page.getByRole('button', { name: /Collapse reviews|Expand reviews/i });
			await expect(reviewToggle).toHaveCount(1);
			const initiallyExpanded = await reviewToggle.getAttribute('aria-expanded');
			if (initiallyExpanded !== 'true') {
				await reviewToggle.evaluate((btn) => (btn as HTMLElement).click());
				await expect(reviewToggle).toHaveAttribute('aria-expanded', 'true', { timeout: 5_000 });
			}

			// PlanReviewCard renders with a "Plan Reviewer" title chip and the
			// verdict text. Summary should be rendered verbatim from the API.
			await expect(page.getByText('Plan Reviewer')).toHaveCount(1);
			const verdictLabel =
				reviewed.review_verdict === 'approved'
					? 'Approved'
					: reviewed.review_verdict === 'needs_changes'
						? 'Needs Changes'
						: reviewed.review_verdict;
			await expect(page.getByText(verdictLabel!, { exact: true }).first()).toHaveCount(1);
			await expect(page.getByText(reviewed.review_summary!.slice(0, 60))).toHaveCount(1);

			// And the OLD bug repro: the "No review results available" empty
			// state must NOT be rendered anywhere. The new copy ("Code review
			// not run yet") may still appear for the implementation-review
			// subsection — that's fine; the R1 card above is what matters.
			const oldEmptyCopy = page.getByText('No review results available');
			await expect(oldEmptyCopy).toHaveCount(0);
		} finally {
			await mockLLM.setDelay(0).catch(() => {});
			await deletePlan(plan.slug).catch(() => {});
		}
	});

	test('exactly one Create Requirements button at the reviewed gate', async ({ page }) => {
		// Regression caught 2026-05-19 during demo: ActionBar AND PlanDetail's
		// guidance hint each rendered their own "Create Requirements" button
		// after the plan reached `reviewed`. Two buttons → user confusion. The
		// guidance-hint version is the surviving one because it carries
		// contextual copy ("Review the plan details, then create requirements
		// and scenarios."); ActionBar lost its bare duplicate.
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('hello-world');

		const plan = await createPlan(`Single-CTA UX ${Date.now()}`);

		try {
			const deadline = Date.now() + 15_000;
			let reviewed = await getPlan(plan.slug);
			while (Date.now() < deadline && !reviewed.review_verdict) {
				await new Promise((r) => setTimeout(r, 250));
				reviewed = await getPlan(plan.slug);
			}
			expect(reviewed.review_verdict).toBeTruthy();

			await page.goto(`/plans/${plan.slug}`);
			await waitForHydration(page);

			await expect(page.getByRole('button', { name: /Create Requirements/i })).toHaveCount(1);
		} finally {
			await deletePlan(plan.slug).catch(() => {});
		}
	});
});
