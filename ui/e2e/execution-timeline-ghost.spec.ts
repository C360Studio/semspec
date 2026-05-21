/**
 * @t0 ExecutionTimeline workflow-roadmap behavior.
 *
 * Pins 2026-05-21 fixes:
 *   1. Both Planning + Execution sections render from plan creation onward,
 *      even when empty. Empty variant is a NON-INTERACTIVE ghost (no
 *      chevron, no click affordance, dashed border, dimmed). Previously the
 *      section was hidden when empty; users couldn't see the upcoming step
 *      and wondered if they were holding something to look at.
 *   2. `architecture-generation` lands in PLANNING, not Execution.
 *      `PLAN_STEPS` was missing it, so the loop fell through to the
 *      Execution filter.
 *
 * Uses the /e2e-test/execution-timeline harness (ssr=false) so no backend
 * is needed — same pattern as trajectory-entry-expand.spec.ts.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 execution-timeline ghost + categorization', () => {
	test('both sections render as ghost when no loops have run', async ({ page }) => {
		await page.goto('/e2e-test/execution-timeline?scenario=empty');
		await waitForHydration(page);

		// Both phase sections are present.
		const sections = page.locator('.phase-section');
		await expect(sections).toHaveCount(2);

		// Both are ghost — the class controls dashed border + dimmed opacity.
		await expect(sections.nth(0)).toHaveClass(/phase-section--ghost/);
		await expect(sections.nth(1)).toHaveClass(/phase-section--ghost/);

		// Title rendered for both, with "Not started" sub-label.
		await expect(page.getByText('Planning', { exact: true })).toBeVisible();
		await expect(page.getByText('Execution', { exact: true })).toBeVisible();
		await expect(page.locator('.phase-count--ghost').filter({ hasText: 'Not started' })).toHaveCount(2);

		// No interactive header — ghost variant uses a div, not a button.
		// The interactive header is the toggle that flips aria-expanded.
		await expect(page.locator('.phase-header').filter({ has: page.locator('[aria-expanded]') })).toHaveCount(0);
	});

	test('Planning becomes interactive when loops arrive; Execution stays ghost', async ({ page }) => {
		await page.goto('/e2e-test/execution-timeline?scenario=planning-loops');
		await waitForHydration(page);

		const sections = page.locator('.phase-section');
		await expect(sections).toHaveCount(2);

		// Planning is interactive (no ghost class).
		await expect(sections.nth(0)).not.toHaveClass(/phase-section--ghost/);
		// Execution stays ghost — no loops there yet.
		await expect(sections.nth(1)).toHaveClass(/phase-section--ghost/);

		// Planning header should be a real button with the loop count.
		await expect(page.getByRole('button').filter({ hasText: /Planning.*2 loops/i })).toBeVisible();

		// Execution still shows "Not started".
		await expect(
			sections.nth(1).locator('.phase-count--ghost').filter({ hasText: 'Not started' })
		).toBeVisible();
	});

	test('architecture-generation lands under Planning, not Execution', async ({ page }) => {
		// Regression pin for the 2026-05-21 fix — PLAN_STEPS was missing
		// `architecture-generation`, so the loop fell into executionLoops.
		// With architecture in PLAN_STEPS, Execution should remain empty
		// (ghost) and Planning should report 4 loops.
		await page.goto('/e2e-test/execution-timeline?scenario=architecture-in-plan');
		await waitForHydration(page);

		const sections = page.locator('.phase-section');
		await expect(sections).toHaveCount(2);

		// Planning has all four loops, including architecture-generation.
		await expect(page.getByRole('button').filter({ hasText: /Planning.*4 loops/i })).toBeVisible();

		// Execution stays ghost — zero loops landed there.
		await expect(sections.nth(1)).toHaveClass(/phase-section--ghost/);
		await expect(
			sections.nth(1).locator('.phase-count--ghost').filter({ hasText: 'Not started' })
		).toBeVisible();
	});

	test('both sections become interactive when execution begins', async ({ page }) => {
		await page.goto('/e2e-test/execution-timeline?scenario=execution-loops');
		await waitForHydration(page);

		const sections = page.locator('.phase-section');
		await expect(sections).toHaveCount(2);
		await expect(sections.nth(0)).not.toHaveClass(/phase-section--ghost/);
		await expect(sections.nth(1)).not.toHaveClass(/phase-section--ghost/);

		await expect(page.getByRole('button').filter({ hasText: /Planning.*2 loops/i })).toBeVisible();
		await expect(page.getByRole('button').filter({ hasText: /Execution.*1 loop/i })).toBeVisible();
	});
});
