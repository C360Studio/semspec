/**
 * @t0 truth-test for bug #7.10 — trajectory entries can be expanded inside
 * ExecutionTimeline's compact layout.
 *
 * Before: `TrajectoryEntryCard` gated both the expand button and the
 * preview block on `!compact`. ExecutionTimeline passes `compact` on every
 * entry in its phase timeline, so the plan page surfaced LLM/tool metadata
 * (tokens, duration, context utilization) but no path to see the actual
 * prompt / response / tool arguments / tool result.
 *
 * Now: `compact` is styling-only; expansion is always available when the
 * entry has a preview payload.
 *
 * Uses the /e2e-test/trajectory-entry harness (ssr=false) so no live
 * backend or SSE seeding is needed — same pattern as the plan-workspace
 * and plan-card harnesses.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 trajectory-entry expansion', () => {
	test('tool-call entry reveals arguments + result on expand (compact layout)', async ({
		page
	}) => {
		await page.goto('/e2e-test/trajectory-entry?scenario=tool-call');
		await waitForHydration(page);

		const card = page.getByTestId('trajectory-entry');
		await expect(card).toBeVisible();
		await expect(card).toHaveAttribute('data-step-type', 'tool_call');

		// Preview hidden before expand — the regression pin.
		await expect(page.getByTestId('entry-preview')).toHaveCount(0);

		const expandBtn = page.getByTestId('entry-expand-btn');
		await expect(expandBtn).toBeVisible();
		await expect(expandBtn).toHaveAttribute('aria-expanded', 'false');
		await expandBtn.click();

		const preview = page.getByTestId('entry-preview');
		await expect(preview).toBeVisible();
		// Both halves of a tool call — the args JSON and the result text.
		await expect(preview).toContainText('Arguments');
		await expect(preview).toContainText('go test ./...');
		await expect(preview).toContainText('Result');
		await expect(preview).toContainText('PASS');

		// aria-expanded reflects state so screen readers announce it.
		await expect(expandBtn).toHaveAttribute('aria-expanded', 'true');
	});

	test('model-call entry reveals response text on expand (compact layout)', async ({ page }) => {
		await page.goto('/e2e-test/trajectory-entry?scenario=model-call');
		await waitForHydration(page);

		const card = page.getByTestId('trajectory-entry');
		await expect(card).toHaveAttribute('data-step-type', 'model_call');

		await page.getByTestId('entry-expand-btn').click();
		const preview = page.getByTestId('entry-preview');
		await expect(preview).toContainText('I will run the tests');
		// Model calls don't carry tool_arguments — only the response pane.
		await expect(preview).not.toContainText('Arguments');
	});

	test('entry with no preview payload hides the expand button', async ({ page }) => {
		// Boundary pin: if hasPreview is false, the button MUST NOT render.
		// This keeps the expand affordance honest — we don't advertise a
		// click on cards where there's nothing to reveal.
		await page.goto('/e2e-test/trajectory-entry?scenario=no-preview');
		await waitForHydration(page);

		const card = page.getByTestId('trajectory-entry');
		await expect(card).toBeVisible();
		await expect(page.getByTestId('entry-expand-btn')).toHaveCount(0);
		await expect(page.getByTestId('entry-preview')).toHaveCount(0);
	});
});
