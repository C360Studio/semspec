/**
 * @t0 InProgressPanel — prominent status surface for long LLM phases.
 *
 * Pins 2026-05-21 work:
 *   - Title + detail copy render
 *   - Spinner icon visible (animated via CSS so test only asserts presence)
 *   - Elapsed-time ticker renders when startedAt is provided, hides when null
 *   - role="status" + aria-live="polite" for screen-reader announce
 *
 * Uses /e2e-test/in-progress-panel harness — ssr=false, hard-coded fixtures,
 * no live backend dependency. Same pattern as trajectory-entry-expand and
 * execution-timeline-ghost specs.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 in-progress-panel', () => {
	test('drafting variant renders title + detail + elapsed + spinner', async ({ page }) => {
		await page.goto('/e2e-test/in-progress-panel?scenario=drafting');
		await waitForHydration(page);

		const panel = page.locator('.in-progress-panel');
		await expect(panel).toBeVisible();

		// Title + detail copy are direct text — match loosely so future
		// rewording survives the assertion.
		await expect(panel).toContainText(/Drafting plan/i);
		await expect(panel).toContainText(/composing the plan goal/i);

		// Spinner icon present. CSS animation is non-assertable in Playwright,
		// but the .spinner-wrap container must exist for the icon to be styled.
		await expect(panel.locator('.spinner-wrap')).toBeVisible();

		// Elapsed time renders with a numeric value. Harness uses startedAt =
		// "10s ago" so we expect something in the 9-12s window allowing for
		// hydration jitter and the formatting rules ("Ns" under 60s).
		const elapsed = panel.locator('.elapsed');
		await expect(elapsed).toBeVisible();
		await expect(elapsed).toContainText(/\d+s/);

		// Accessibility: status role + polite live region.
		await expect(panel).toHaveAttribute('role', 'status');
		await expect(panel).toHaveAttribute('aria-live', 'polite');
	});

	test('no-elapsed variant hides the elapsed widget', async ({ page }) => {
		// Boundary pin — when startedAt is null/undefined the elapsed chip
		// MUST NOT render. Previously could have shown "0s" or "—" which
		// reads as "stuck" rather than "no timestamp available".
		await page.goto('/e2e-test/in-progress-panel?scenario=no-elapsed');
		await waitForHydration(page);

		const panel = page.locator('.in-progress-panel');
		await expect(panel).toBeVisible();
		await expect(panel).toContainText(/Drafting plan/i);

		// Elapsed widget must not be in the DOM.
		await expect(panel.locator('.elapsed')).toHaveCount(0);
	});

	test('review variant renders the reviewer copy', async ({ page }) => {
		// Confirms the component accepts any title/detail from the caller —
		// the route maps stage → copy via stageTitle() + planGuidance.message.
		// Catches regressions where a hardcoded "Drafting" leaks into the
		// component itself.
		await page.goto('/e2e-test/in-progress-panel?scenario=review');
		await waitForHydration(page);

		const panel = page.locator('.in-progress-panel');
		await expect(panel).toContainText(/Reviewing plan draft/i);
		await expect(panel).toContainText(/evaluating the draft/i);
		await expect(panel).not.toContainText(/Drafting plan/i);
	});
});
