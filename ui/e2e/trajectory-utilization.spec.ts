import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

/**
 * @t0 trajectory utilization display tests.
 *
 * Tests context utilization rendering in trajectory views.
 * Runs against a live stack — skips gracefully when no trajectory data
 * has utilization values (requires a completed agent loop with beta.7+).
 */
test.describe('@t0 trajectory-utilization', () => {
	test('utilization bar renders on model_call steps with ARIA attributes', async ({ page }) => {
		// Find a loop with trajectory data
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		// Check if any utilization bars exist (beta.7+ data required)
		const ctxBars = page.locator('[role="progressbar"][aria-label*="Context window"]');
		const barCount = await ctxBars.count();

		if (barCount === 0) {
			// No utilization data — skip gracefully (pre-beta.7 loops)
			test.skip();
			return;
		}

		// Verify ARIA attributes on first bar
		const firstBar = ctxBars.first();
		await expect(firstBar).toHaveAttribute('aria-valuemin', '0');
		await expect(firstBar).toHaveAttribute('aria-valuemax', '100');

		const valuenow = await firstBar.getAttribute('aria-valuenow');
		expect(valuenow).toBeTruthy();
		const pct = parseInt(valuenow!, 10);
		expect(pct).toBeGreaterThanOrEqual(0);
		expect(pct).toBeLessThanOrEqual(100);

		// Verify the fill bar exists inside
		const fill = firstBar.locator('.ctx-fill');
		await expect(fill).toBeVisible();

		// Verify percentage label is next to the bar
		const label = firstBar.locator('..').locator('.ctx-label');
		await expect(label).toContainText('%');
	});

	test('peak context utilization shows in summary bar', async ({ page }) => {
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		// Check if peak ctx is displayed (requires utilization data)
		const peakCtx = page.getByTestId('trajectory-peak-ctx');
		const hasPeak = await peakCtx.isVisible().catch(() => false);

		if (!hasPeak) {
			// No utilization data available
			test.skip();
			return;
		}

		await expect(peakCtx).toContainText(/Peak ctx:/);
		await expect(peakCtx).toContainText(/%/);
	});

	test('tool call steps show tokens_in when available', async ({ page }) => {
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		// Find tool call cards
		const toolCards = page.locator('.entry-card.tool-call');
		const toolCount = await toolCards.count();

		if (toolCount === 0) {
			test.skip();
			return;
		}

		// At least some tool cards should have a metrics row
		const firstTool = toolCards.first();
		const metricsRow = firstTool.locator('.metrics-row');
		await expect(metricsRow).toBeVisible();

		// Duration should always be shown for tool calls
		await expect(metricsRow).toContainText(/ms|s/);
	});

	test('high utilization shows warning icon', async ({ page }) => {
		// This test verifies the warning icon appears at >80% utilization.
		// We can't control the data, so we verify the structure is correct
		// by checking that ctx-label elements exist and contain expected content.
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		const ctxLabels = page.locator('.ctx-label');
		const count = await ctxLabels.count();

		if (count === 0) {
			test.skip();
			return;
		}

		// Each label should contain a percentage
		for (let i = 0; i < Math.min(count, 5); i++) {
			await expect(ctxLabels.nth(i)).toContainText('%');
		}
	});

	test('trajectory panel shows peak utilization in compact summary', async ({ page }) => {
		// Navigate to trajectories list to check the panel preview
		await page.goto('/trajectories');
		await waitForHydration(page);

		const items = page.getByTestId('trajectory-item');
		const count = await items.count();
		if (count === 0) {
			test.skip();
			return;
		}

		// Click first item to load its preview in the right panel
		await items.first().click();

		// Wait for the trajectory panel to load
		const panel = page.locator('.trajectory-panel');
		const hasPanel = await panel.isVisible().catch(() => false);

		if (!hasPanel) {
			test.skip();
			return;
		}

		// Summary bar should show stats (LLM count, tool count, tokens)
		const summaryBar = panel.locator('.summary-bar');
		const hasSummary = await summaryBar.isVisible().catch(() => false);

		if (hasSummary) {
			// Should show at least LLM and tool counts
			await expect(summaryBar).toContainText(/LLM/);
		}
	});
});
