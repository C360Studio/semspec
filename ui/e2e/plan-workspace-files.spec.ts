/**
 * @t0 truth-tests for PlanWorkspace (bug #7.6).
 *
 * Before: the Files view showed task-level worktree UUIDs as labels, rendered
 * the fixture tree instead of the agent's diff, and dropped most of the
 * requirement branches on the floor.
 *
 * Now: per-requirement branch summary from /plans/{slug}/branches, titles as
 * labels, changed-file list with +/- stats, and unified-diff in the viewer.
 * Each assertion below pins one of the regressions that must not return.
 *
 * The component is rendered via /e2e-test/plan-workspace?slug=X (ssr=false)
 * so Playwright stubs intercept every fetch — matching the /status pattern.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { stubPlanBranches, stubRequirementFileDiff } from './helpers/truth';

const slug = 'files-fixture';

test.describe('@t0 plan-workspace files view', () => {
	test('dropdown labels are requirement titles, not branch UUIDs', async ({ page }) => {
		await stubPlanBranches(page, slug, [
			{
				requirement_id: 'R1',
				title: 'Parse mortgage input',
				branch: 'semspec/requirement-R1',
				stage: 'completed',
				files: [{ path: 'src/parser.go', status: 'added', insertions: 30, deletions: 0 }]
			},
			{
				requirement_id: 'R2',
				title: 'Compute amortization',
				branch: 'semspec/requirement-R2',
				stage: 'completed',
				files: [{ path: 'src/amort.go', status: 'added', insertions: 42, deletions: 0 }]
			}
		]);
		await stubRequirementFileDiff(page, slug, {});

		await page.goto(`/e2e-test/plan-workspace?slug=${slug}`);
		await waitForHydration(page);

		const select = page.getByTestId('requirement-select');
		await expect(select).toBeVisible();

		// Options must carry the human title and the requirement ID — not just
		// the branch name (the bug was showing `agent/UUID` labels).
		const optionTexts = await select.locator('option').allTextContents();
		expect(optionTexts).toContain('[R1] Parse mortgage input');
		expect(optionTexts).toContain('[R2] Compute amortization');
	});

	test('all requirement branches appear — no silent truncation', async ({ page }) => {
		// Regression test for "only 2 of 6 branches shown" — the old
		// `branch.includes(slug)` filter dropped most worktrees. The new
		// endpoint returns one row per plan requirement, so all six must show.
		const six = Array.from({ length: 6 }, (_, i) => ({
			requirement_id: `R${i + 1}`,
			title: `Requirement ${i + 1}`,
			branch: `semspec/requirement-R${i + 1}`,
			stage: 'completed',
			files: [{ path: `src/file-${i + 1}.go`, status: 'added', insertions: 10, deletions: 0 }]
		}));
		await stubPlanBranches(page, slug, six);
		await stubRequirementFileDiff(page, slug, {});

		await page.goto(`/e2e-test/plan-workspace?slug=${slug}`);
		await waitForHydration(page);

		const options = page.getByTestId('requirement-select').locator('option');
		await expect(options).toHaveCount(6);
	});

	test('file list renders changed files with +/- stats, not the fixture tree', async ({ page }) => {
		// Regression test for "shows seed file tree instead of agent changes".
		// The old UI rendered `workspace/tree` (everything tracked), which
		// masked the agent's work behind the fixture. The new UI only lists
		// files that appear in the branch diff.
		await stubPlanBranches(page, slug, [
			{
				requirement_id: 'R1',
				title: 'Add parser',
				branch: 'semspec/requirement-R1',
				stage: 'completed',
				files: [
					{ path: 'src/parser.go', status: 'added', insertions: 30, deletions: 0 },
					{ path: 'src/main.go', status: 'modified', insertions: 3, deletions: 1 }
				]
			}
		]);
		await stubRequirementFileDiff(page, slug, {});

		await page.goto(`/e2e-test/plan-workspace?slug=${slug}`);
		await waitForHydration(page);

		// Summary row quotes the count + totals so human can see scope at a
		// glance without clicking through each file.
		const summary = page.getByTestId('file-summary');
		await expect(summary).toContainText('2 files changed');
		await expect(summary).toContainText('+33');
		await expect(summary).toContainText('1'); // total deletions

		// Both changed files appear. Fixture files (like README, go.mod) that
		// the agent didn't touch must NOT appear.
		await expect(page.getByTestId('file-src/parser.go')).toBeVisible();
		await expect(page.getByTestId('file-src/main.go')).toBeVisible();
		await expect(page.getByTestId('file-README.md')).toHaveCount(0);
	});

	test('clicking a file fetches and renders its diff patch', async ({ page }) => {
		await stubPlanBranches(page, slug, [
			{
				requirement_id: 'R1',
				title: 'Add parser',
				branch: 'semspec/requirement-R1',
				stage: 'completed',
				files: [{ path: 'src/parser.go', status: 'added', insertions: 2, deletions: 0 }]
			}
		]);
		await stubRequirementFileDiff(page, slug, {
			'src/parser.go':
				'--- a/src/parser.go\n+++ b/src/parser.go\n@@ -0,0 +1,2 @@\n+package parser\n+\n'
		});

		await page.goto(`/e2e-test/plan-workspace?slug=${slug}`);
		await waitForHydration(page);

		// Auto-select behaviour picks the first requirement with changes, so
		// the file row is available immediately.
		await page.getByTestId('file-src/parser.go').click();

		const diff = page.getByTestId('viewer-diff');
		await expect(diff).toBeVisible();
		// The unified diff's added-line ("+package parser") must survive
		// rendering. This catches any regression to "show full file content
		// instead of diff".
		await expect(diff).toContainText('+package parser');
		await expect(diff).toContainText('@@ -0,0 +1,2 @@');
	});

	test('not-started requirement is labeled and shows empty-state message', async ({ page }) => {
		// A plan with 3 reqs where only 1 has started execution. The remaining
		// two must still appear in the dropdown (so the user sees the complete
		// roadmap) and must self-identify as "not started".
		await stubPlanBranches(page, slug, [
			{
				requirement_id: 'R1',
				title: 'Running',
				branch: 'semspec/requirement-R1',
				stage: 'completed',
				files: [{ path: 'src/done.go', status: 'added', insertions: 1, deletions: 0 }]
			},
			{ requirement_id: 'R2', title: 'Waiting', stage: 'pending' },
			{ requirement_id: 'R3', title: 'Also waiting', stage: 'pending' }
		]);
		await stubRequirementFileDiff(page, slug, {});

		await page.goto(`/e2e-test/plan-workspace?slug=${slug}`);
		await waitForHydration(page);

		const options = page.getByTestId('requirement-select').locator('option');
		await expect(options).toHaveCount(3);
		await expect(options.nth(1)).toContainText('not started');

		// Select a not-started requirement — empty state should explain why.
		await page.getByTestId('requirement-select').selectOption('R2');
		await expect(page.getByTestId('no-changes')).toContainText(
			/hasn't started|no branch/i
		);
	});
});
