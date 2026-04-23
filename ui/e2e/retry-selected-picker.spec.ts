/**
 * @t0 truth-tests for #10 item 1 — per-requirement retry selection.
 *
 * The stalled-plan retry flow used to be a single "Retry Failed" button that
 * reset every failed requirement in one go. Users with mixed failures (e.g.
 * one known-bad-scope requirement they want to skip, one transient model
 * hiccup to retry) had no cherry-pick option. The new picker shows a
 * checkbox per failed requirement and POSTs
 *   { scope: "requirements", requirement_ids: [...] }
 * so completed / running requirements stay untouched.
 *
 * These tests verify: the picker lists ONLY failed/error requirements, the
 * checkbox + submit flow works end-to-end, and the POST body carries the
 * exact selection the user made.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { stubPlanBranches, stubRetry } from './helpers/truth';

const slug = 'retry-fixture';

test.describe('@t0 retry-selected-picker', () => {
	test('lists only failed/error requirements (filters out completed + pending)', async ({
		page
	}) => {
		await stubPlanBranches(page, slug, [
			{ requirement_id: 'R1', title: 'Already done', stage: 'completed' },
			{ requirement_id: 'R2', title: 'Still running', stage: 'decomposing' },
			{ requirement_id: 'R3', title: 'Broke on review', stage: 'failed' },
			{ requirement_id: 'R4', title: 'Broke on exec', stage: 'error' }
		]);
		await stubRetry(page, slug);

		await page.goto(`/e2e-test/retry-picker?slug=${slug}`);
		await waitForHydration(page);

		// Two failure rows exactly — R3 and R4. R1 (completed) and R2
		// (decomposing / in-flight) must NOT appear or the user could
		// accidentally reset a running or completed requirement.
		await expect(page.getByTestId('retry-checkbox-R3')).toBeVisible();
		await expect(page.getByTestId('retry-checkbox-R4')).toBeVisible();
		await expect(page.getByTestId('retry-checkbox-R1')).toHaveCount(0);
		await expect(page.getByTestId('retry-checkbox-R2')).toHaveCount(0);

		await expect(page.getByTestId('retry-selected-count')).toContainText('0 of 2');
	});

	test('submit POSTs only the checked requirement_ids', async ({ page }) => {
		await stubPlanBranches(page, slug, [
			{ requirement_id: 'R3', title: 'Parse input', stage: 'failed' },
			{ requirement_id: 'R4', title: 'Compute total', stage: 'failed' },
			{ requirement_id: 'R5', title: 'Render output', stage: 'error' }
		]);
		const capture = await stubRetry(page, slug);

		await page.goto(`/e2e-test/retry-picker?slug=${slug}`);
		await waitForHydration(page);

		// Pick R3 and R5 but not R4 — the whole point of cherry-picking.
		await page.getByTestId('retry-checkbox-R3').check();
		await page.getByTestId('retry-checkbox-R5').check();
		await expect(page.getByTestId('retry-selected-count')).toContainText('2 of 3');

		const submit = page.getByTestId('retry-submit');
		await expect(submit).toHaveText(/Retry 2 selected/);
		await submit.click();

		// Wait for the `onRetried` callback side-effect so we know the request
		// completed before asserting on the capture.
		await expect(page.getByTestId('last-retried-at')).toBeVisible();

		expect(capture.calls).toBe(1);
		expect(capture.last).toEqual({
			scope: 'requirements',
			requirement_ids: ['R3', 'R5']
		});
	});

	test('select-all toggles every failed requirement and flips back to clear-all', async ({
		page
	}) => {
		await stubPlanBranches(page, slug, [
			{ requirement_id: 'R3', title: 'A', stage: 'failed' },
			{ requirement_id: 'R4', title: 'B', stage: 'failed' },
			{ requirement_id: 'R5', title: 'C', stage: 'error' }
		]);
		await stubRetry(page, slug);

		await page.goto(`/e2e-test/retry-picker?slug=${slug}`);
		await waitForHydration(page);

		const toggle = page.getByTestId('retry-select-all');
		await expect(toggle).toHaveText('Select all');

		await toggle.click();
		await expect(toggle).toHaveText('Clear all');
		await expect(page.getByTestId('retry-selected-count')).toContainText('3 of 3');

		await toggle.click();
		await expect(toggle).toHaveText('Select all');
		await expect(page.getByTestId('retry-selected-count')).toContainText('0 of 3');
	});

	test('submit button is disabled when nothing is selected', async ({ page }) => {
		await stubPlanBranches(page, slug, [
			{ requirement_id: 'R3', title: 'A', stage: 'failed' }
		]);
		await stubRetry(page, slug);

		await page.goto(`/e2e-test/retry-picker?slug=${slug}`);
		await waitForHydration(page);

		await expect(page.getByTestId('retry-submit')).toBeDisabled();
	});

	test('empty state renders when there are no failures', async ({ page }) => {
		await stubPlanBranches(page, slug, [
			{ requirement_id: 'R1', title: 'Done', stage: 'completed' }
		]);
		await stubRetry(page, slug);

		await page.goto(`/e2e-test/retry-picker?slug=${slug}`);
		await waitForHydration(page);

		await expect(page.getByText(/No failed requirements/i)).toBeVisible();
		await expect(page.getByTestId('retry-submit')).toHaveCount(0);
	});
});
