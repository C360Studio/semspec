import { test, expect } from './helpers/setup';
import { createMockReviewResult, createMockFinding } from './helpers/workflow';

test.describe('Review Dashboard', () => {
	test.describe('Reviews Toggle', () => {
		test('shows reviews toggle on executing plan', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'review-toggle-plan',
							title: 'Review Toggle Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/review-toggle-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'review-toggle-plan',
						title: 'Review Toggle Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/review-toggle-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('review-toggle-plan');
			await planDetailPage.expectReviewsSectionVisible();
		});

		test('shows reviews toggle on complete plan', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'complete-review-plan',
							title: 'Complete Review Plan',
							approved: true,
							stage: 'complete',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/complete-review-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'complete-review-plan',
						title: 'Complete Review Plan',
						approved: true,
						stage: 'complete',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/complete-review-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('complete-review-plan');
			await planDetailPage.expectReviewsSectionVisible();
		});

		test('hides reviews toggle on draft plan', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'unapproved-review-plan',
							title: 'Unapproved Plan',
							approved: false,
							stage: 'draft',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/unapproved-review-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'unapproved-review-plan',
						title: 'Unapproved Plan',
						approved: false,
						stage: 'draft',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/unapproved-review-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('unapproved-review-plan');
			await planDetailPage.expectReviewsSectionHidden();
		});

		test('expands to show review dashboard', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'expand-review-plan',
							title: 'Expand Review Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/expand-review-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'expand-review-plan',
						title: 'Expand Review Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/expand-review-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/expand-review-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult())
				});
			});

			await planDetailPage.goto('expand-review-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectReviewsExpanded();
		});

		test('collapses when toggle clicked again', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'collapse-review-plan',
							title: 'Collapse Review Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/collapse-review-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'collapse-review-plan',
						title: 'Collapse Review Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/collapse-review-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/collapse-review-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult())
				});
			});

			await planDetailPage.goto('collapse-review-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectReviewsExpanded();

			await planDetailPage.collapseReviews();
			await planDetailPage.expectReviewsCollapsed();
		});
	});

	test.describe('Spec Compliance Gate', () => {
		test('shows spec gate component', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'spec-gate-plan',
							title: 'Spec Gate Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/spec-gate-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'spec-gate-plan',
						title: 'Spec Gate Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/spec-gate-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/spec-gate-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult())
				});
			});

			await planDetailPage.goto('spec-gate-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectSpecGateVisible();
		});

		test('shows passed state when spec compliant', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'spec-passed-plan',
							title: 'Spec Passed Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/spec-passed-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'spec-passed-plan',
						title: 'Spec Passed Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/spec-passed-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/spec-passed-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						verdict: 'passed',
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'compliant', passed: true, summary: 'All specs compliant' }
						]
					}))
				});
			});

			await planDetailPage.goto('spec-passed-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectSpecGatePassed();
		});

		test('shows failed state when spec non-compliant', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'spec-failed-plan',
							title: 'Spec Failed Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/spec-failed-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'spec-failed-plan',
						title: 'Spec Failed Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/spec-failed-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/spec-failed-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						verdict: 'failed',
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'non_compliant', passed: false, summary: 'Missing required fields' }
						]
					}))
				});
			});

			await planDetailPage.goto('spec-failed-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectSpecGateFailed();
		});
	});

	test.describe('Quality Reviewer Cards', () => {
		test('shows reviewer cards for quality reviewers', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'reviewer-cards-plan',
							title: 'Reviewer Cards Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/reviewer-cards-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'reviewer-cards-plan',
						title: 'Reviewer Cards Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/reviewer-cards-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/reviewer-cards-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'compliant', passed: true },
							{ role: 'sop_reviewer', verdict: 'approved', passed: true },
							{ role: 'style_reviewer', verdict: 'approved', passed: true },
							{ role: 'security_reviewer', verdict: 'approved', passed: true }
						],
						stats: {
							total_findings: 0,
							by_severity: {},
							by_reviewer: {},
							reviewers_total: 4,
							reviewers_passed: 4
						}
					}))
				});
			});

			await planDetailPage.goto('reviewer-cards-plan');
			await planDetailPage.expandReviews();

			// Should show quality reviewer cards (not spec_reviewer which is in gate)
			await planDetailPage.expectReviewerCount(3); // sop, style, security
		});

		test('shows pass/fail indicators on reviewer cards', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'pass-fail-plan',
							title: 'Pass Fail Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/pass-fail-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'pass-fail-plan',
						title: 'Pass Fail Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/pass-fail-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/pass-fail-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						verdict: 'failed',
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'compliant', passed: true },
							{ role: 'sop_reviewer', verdict: 'approved', passed: true },
							{ role: 'style_reviewer', verdict: 'rejected', passed: false },
							{ role: 'security_reviewer', verdict: 'approved', passed: true }
						],
						stats: {
							total_findings: 2,
							by_severity: { critical: 1, high: 1 },
							by_reviewer: { security_reviewer: 1, style_reviewer: 1 },
							reviewers_total: 4,
							reviewers_passed: 3
						}
					}))
				});
			});

			await planDetailPage.goto('pass-fail-plan');
			await planDetailPage.expandReviews();

			// Check for pass/fail classes on reviewer cards
			const reviewerCards = page.locator('.reviewer-card');
			await expect(reviewerCards.filter({ hasClass: /passed|failed/ }).first()).toBeVisible();
		});

		test('shows reviewer stats', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'stats-plan',
							title: 'Stats Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/stats-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'stats-plan',
						title: 'Stats Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/stats-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/stats-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'compliant', passed: true },
							{ role: 'sop_reviewer', verdict: 'approved', passed: true },
							{ role: 'style_reviewer', verdict: 'approved', passed: true },
							{ role: 'security_reviewer', verdict: 'rejected', passed: false }
						],
						stats: {
							total_findings: 3,
							by_severity: { critical: 1, high: 2 },
							by_reviewer: { security_reviewer: 1, style_reviewer: 2 },
							reviewers_total: 4,
							reviewers_passed: 3
						}
					}))
				});
			});

			await planDetailPage.goto('stats-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectReviewerStats(3, 4);
		});
	});

	test.describe('Findings List', () => {
		test('displays findings with severity', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'findings-plan',
							title: 'Findings Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/findings-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'findings-plan',
						title: 'Findings Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/findings-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/findings-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						findings: [
							createMockFinding({ severity: 'critical', issue: 'Critical security issue', file: 'src/auth.ts', line: 42, role: 'security_reviewer' }),
							createMockFinding({ severity: 'high', issue: 'Inconsistent naming', file: 'src/utils.ts', line: 15, role: 'style_reviewer' }),
							createMockFinding({ severity: 'medium', issue: 'Consider adding documentation', role: 'sop_reviewer' })
						],
						stats: {
							total_findings: 3,
							by_severity: { critical: 1, high: 1, medium: 1 },
							by_reviewer: { security_reviewer: 1, style_reviewer: 1, sop_reviewer: 1 },
							reviewers_total: 4,
							reviewers_passed: 3
						}
					}))
				});
			});

			await planDetailPage.goto('findings-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectFindingsListVisible();
			await planDetailPage.expectFindingsCount(3);
		});

		test('shows file and line references in findings', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'file-refs-plan',
							title: 'File Refs Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/file-refs-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'file-refs-plan',
						title: 'File Refs Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/file-refs-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/file-refs-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						findings: [
							createMockFinding({
								severity: 'critical',
								issue: 'SQL injection vulnerability',
								file: 'src/db/queries.ts',
								line: 127,
								role: 'security_reviewer',
								cwe: 'CWE-89'
							})
						],
						stats: {
							total_findings: 1,
							by_severity: { critical: 1 },
							by_reviewer: { security_reviewer: 1 },
							reviewers_total: 4,
							reviewers_passed: 3
						}
					}))
				});
			});

			await planDetailPage.goto('file-refs-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectFindingsListVisible();

			// Check for file reference in finding
			const finding = page.locator('.finding-row').first();
			await expect(finding).toContainText('src/db/queries.ts');
		});
	});

	test.describe('Empty and Error States', () => {
		test('handles empty reviews gracefully', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'empty-reviews-plan',
							title: 'Empty Reviews Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/empty-reviews-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'empty-reviews-plan',
						title: 'Empty Reviews Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/empty-reviews-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			// Return empty/null response for reviews (no reviews yet)
			await page.route('**/workflow-api/plans/empty-reviews-plan/reviews', route => {
				// Return null/undefined to simulate no reviews yet
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: 'null'
				});
			});

			await planDetailPage.goto('empty-reviews-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectEmptyReviews();
		});

		test('shows error state when reviews fetch fails', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'error-reviews-plan',
							title: 'Error Reviews Plan',
							approved: true,
							stage: 'executing',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/error-reviews-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'error-reviews-plan',
						title: 'Error Reviews Plan',
						approved: true,
						stage: 'executing',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/error-reviews-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			// Return 500 for reviews
			await page.route('**/workflow-api/plans/error-reviews-plan/reviews', route => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'Internal server error' })
				});
			});

			await planDetailPage.goto('error-reviews-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectReviewError();
		});
	});

	test.describe('Verdict Badge', () => {
		test('shows passed verdict badge', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'passed-verdict-plan',
							title: 'Passed Verdict Plan',
							approved: true,
							stage: 'complete',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/passed-verdict-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'passed-verdict-plan',
						title: 'Passed Verdict Plan',
						approved: true,
						stage: 'complete',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/passed-verdict-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/passed-verdict-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						verdict: 'passed'
					}))
				});
			});

			await planDetailPage.goto('passed-verdict-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectVerdictBadge('passed');
		});

		test('shows failed verdict badge', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'failed-verdict-plan',
							title: 'Failed Verdict Plan',
							approved: true,
							stage: 'complete',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/failed-verdict-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'failed-verdict-plan',
						title: 'Failed Verdict Plan',
						approved: true,
						stage: 'complete',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/failed-verdict-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/failed-verdict-plan/reviews', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify(createMockReviewResult({
						verdict: 'failed',
						reviewers: [
							{ role: 'spec_reviewer', verdict: 'non_compliant', passed: false }
						],
						stats: {
							total_findings: 5,
							by_severity: { critical: 3, high: 2 },
							by_reviewer: { spec_reviewer: 5 },
							reviewers_total: 4,
							reviewers_passed: 1
						}
					}))
				});
			});

			await planDetailPage.goto('failed-verdict-plan');
			await planDetailPage.expandReviews();
			await planDetailPage.expectVerdictBadge('failed');
		});
	});
});
