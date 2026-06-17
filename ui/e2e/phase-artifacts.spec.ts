/**
 * @t0 @smoke truth-tests for PhaseArtifactsView (PR #4).
 *
 * Backend writes per-phase markdown to .semspec/plans/{slug}/{plan,architecture,
 * requirements,scenarios,qa-summary,run-summary}.md. The viewer fetches the
 * list, concurrent-fetches each body, renders inline with `marked`, sanitizes
 * with DOMPurify, and DOM-rewrites heading anchors + ./X.md cross-links.
 *
 * Unit tests cover the pure renderer end-to-end (artifactRenderer.test.ts).
 * These specs pin the integration: API shape → DOM, TOC click → in-page nav,
 * empty + error states. Stubs only — no LLM, no docker filesystem seeding.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import {
	stubPhaseArtifactContent,
	stubPhaseArtifacts,
	stubProjectConfigured
} from './helpers/truth';

const slug = 'artifacts-fixture';

test.describe('@t0 @smoke phase-artifacts view', () => {
	test.beforeEach(async ({ page }) => {
		await stubProjectConfigured(page);
	});

	test('empty list renders the "not written yet" state', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, []);
		await stubPhaseArtifactContent(page, slug, {});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		const container = page.getByTestId('phase-artifacts');
		await expect(container).toBeVisible();
		await expect(container).toContainText('No phase artifacts written yet');
	});

	test('TOC chips render in canonical order and each artifact has a section', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, [
			{ name: 'plan' },
			{ name: 'architecture' },
			{ name: 'requirements' }
		]);
		await stubPhaseArtifactContent(page, slug, {
			plan: '# Plan\n\nBody.\n',
			architecture: '# Architecture\n\n## Technology choices\n\nBody.\n',
			requirements: '# Requirements\n\nBody.\n'
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		// One chip per artifact, with the human label.
		const chipLabels = await page.locator('.toc-chip').allTextContents();
		expect(chipLabels.map((s) => s.trim())).toEqual([
			'Plan',
			'Architecture',
			'Requirements'
		]);

		// Each artifact lands in its own labelled section with the canonical id.
		await expect(page.locator('section#artifact-plan')).toBeVisible();
		await expect(page.locator('section#artifact-architecture')).toBeVisible();
		await expect(page.locator('section#artifact-requirements')).toBeVisible();
	});

	test('GFM tables render through marked + sanitization', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, [{ name: 'architecture' }]);
		await stubPhaseArtifactContent(page, slug, {
			architecture:
				'# Architecture\n\n' +
				'| Category | Choice |\n|---|---|\n| Lang | Go |\n| Db | sqlite |\n'
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		const table = page.locator('section#artifact-architecture table');
		await expect(table).toBeVisible();
		await expect(table.locator('th').nth(0)).toHaveText('Category');
		await expect(table.locator('td').filter({ hasText: 'Go' })).toBeVisible();
	});

	test('headings carry deterministic anchor ids', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, [{ name: 'architecture' }]);
		await stubPhaseArtifactContent(page, slug, {
			architecture: '# Architecture\n\n## Technology choices\n\nBody.\n'
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		// renderArtifact emits ${artifact}--${slug} ids on every heading inside.
		await expect(
			page.locator('section#artifact-architecture h2#architecture--technology-choices')
		).toBeVisible();
	});

	test('cross-artifact links are rewritten to in-page anchors', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, [
			{ name: 'run-summary' },
			{ name: 'architecture' }
		]);
		await stubPhaseArtifactContent(page, slug, {
			'run-summary':
				'# Run summary\n\nSee [architecture](./architecture.md) for details.\n',
			architecture: '# Architecture\n\nBody.\n'
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		const link = page.locator('section#artifact-run-summary a', { hasText: 'architecture' });
		await expect(link).toHaveAttribute('href', '#artifact-architecture');
	});

	test('clicking a TOC chip jumps to the matching section', async ({ page }) => {
		await stubPhaseArtifacts(page, slug, [
			{ name: 'plan' },
			{ name: 'architecture' },
			{ name: 'requirements' }
		]);
		// Pad bodies so scroll position actually changes between sections.
		const filler = '\n\n' + 'Lorem ipsum dolor sit amet.\n\n'.repeat(40);
		await stubPhaseArtifactContent(page, slug, {
			plan: '# Plan' + filler,
			architecture: '# Architecture' + filler,
			requirements: '# Requirements' + filler
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		const requirementsSection = page.locator('section#artifact-requirements');
		await page.getByTestId('toc-requirements').click();

		// scrollIntoView with smooth behavior isn't instant — wait for the
		// section to actually enter the viewport rather than asserting on
		// scrollY which is brittle across viewport sizes.
		await expect(requirementsSection).toBeInViewport();
	});

	test('list endpoint failure surfaces an error banner', async ({ page }) => {
		await page.route(`**/plan-manager/plans/${slug}/artifacts`, (route) => {
			route.fulfill({
				status: 500,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'planner unavailable' })
			});
		});

		await page.goto(`/e2e-test/phase-artifacts?slug=${slug}`);
		await waitForHydration(page);

		const banner = page.getByTestId('phase-artifacts').locator('[role="alert"]');
		await expect(banner).toBeVisible();
		await expect(banner).toContainText('planner unavailable');
	});
});
