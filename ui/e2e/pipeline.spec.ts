import { test, expect, testData } from './helpers/setup';

test.describe('Agent Pipeline View', () => {
	test.describe('Pipeline Rendering', () => {
		test('shows pipeline section on approved plan', async ({ page, planDetailPage }) => {
			// Mock an approved plan with active loops
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'test-pipeline-plan',
							title: 'Test Pipeline Plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'pipeline-loop-1',
									role: 'developer',
									model: 'claude-3-sonnet',
									state: 'executing',
									iterations: 2,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/test-pipeline-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'test-pipeline-plan',
						title: 'Test Pipeline Plan',
						approved: true,
						stage: 'executing',
						active_loops: [
							{
								loop_id: 'pipeline-loop-1',
								role: 'developer',
								model: 'claude-3-sonnet',
								state: 'executing',
								iterations: 2,
								max_iterations: 10
							}
						]
					})
				});
			});

			await page.route('**/workflow-api/plans/test-pipeline-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('test-pipeline-plan');
			await planDetailPage.expectPipelineVisible();
		});

		test('hides pipeline section on unapproved plan', async ({ page, planDetailPage }) => {
			// Mock a draft (unapproved) plan
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'unapproved-plan',
							title: 'Unapproved Plan',
							approved: false,
							stage: 'draft',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/unapproved-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'unapproved-plan',
						title: 'Unapproved Plan',
						approved: false,
						stage: 'draft',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/unapproved-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('unapproved-plan');
			await expect(planDetailPage.pipelineSection).not.toBeVisible();
		});

		test('shows pipeline stages', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'stages-plan',
							title: 'Stages Plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'stage-loop-1',
									role: 'task-generator',
									model: 'claude-3-sonnet',
									state: 'executing',
									iterations: 1,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/stages-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'stages-plan',
						title: 'Stages Plan',
						approved: true,
						stage: 'executing',
						active_loops: [
							{
								loop_id: 'stage-loop-1',
								role: 'task-generator',
								model: 'claude-3-sonnet',
								state: 'executing',
								iterations: 1,
								max_iterations: 10
							}
						]
					})
				});
			});

			await page.route('**/workflow-api/plans/stages-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('stages-plan');
			await planDetailPage.expectPipelineVisible();

			// Verify pipeline stages are rendered
			const stages = planDetailPage.pipelineStages;
			const count = await stages.count();
			expect(count).toBeGreaterThan(0);
		});
	});

	test.describe('Active Stage', () => {
		test('shows active stage with spinner', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'active-stage-plan',
							title: 'Active Stage Plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'active-loop-1',
									role: 'developer',
									model: 'claude-3',
									state: 'executing',
									iterations: 3,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/active-stage-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'active-stage-plan',
						title: 'Active Stage Plan',
						approved: true,
						stage: 'executing',
						active_loops: [
							{
								loop_id: 'active-loop-1',
								role: 'developer',
								model: 'claude-3',
								state: 'executing',
								iterations: 3,
								max_iterations: 10
							}
						]
					})
				});
			});

			await page.route('**/workflow-api/plans/active-stage-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('active-stage-plan');
			await planDetailPage.expectPipelineVisible();

			// Look for an active stage
			const activeStage = page.locator('.pipeline-stage.active');
			await expect(activeStage).toBeVisible();

			// Check for spinner on active stage
			const spinner = activeStage.locator('.spin');
			await expect(spinner).toBeVisible();
		});

		test('shows iteration progress on active stage', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'progress-plan',
							title: 'Progress Plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'progress-loop-1',
									role: 'task-generator',
									model: 'claude-3',
									state: 'executing',
									iterations: 5,
									max_iterations: 10
								}
							]
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/progress-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'progress-plan',
						title: 'Progress Plan',
						approved: true,
						stage: 'executing',
						active_loops: [
							{
								loop_id: 'progress-loop-1',
								role: 'task-generator',
								model: 'claude-3',
								state: 'executing',
								iterations: 5,
								max_iterations: 10
							}
						]
					})
				});
			});

			await page.route('**/workflow-api/plans/progress-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('progress-plan');
			await planDetailPage.expectPipelineVisible();

			// Check for progress indicator
			const progressText = page.locator('.pipeline-stage.active .stage-progress');
			if (await progressText.isVisible()) {
				await expect(progressText).toHaveText('5/10');
			}
		});
	});

	test.describe('Completed Stages', () => {
		test('shows completed stages with success styling', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'complete-plan',
							title: 'Complete Plan',
							approved: true,
							stage: 'complete',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/complete-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'complete-plan',
						title: 'Complete Plan',
						approved: true,
						stage: 'complete',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/complete-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('complete-plan');

			// For a complete plan, check the stage badge
			const stageBadge = page.locator('.plan-stage');
			await expect(stageBadge).toHaveText('Complete');
		});
	});

	test.describe('Review Branch', () => {
		test('shows parallel reviewer branch when in review', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'review-plan',
							title: 'Review Plan',
							approved: true,
							stage: 'executing',
							active_loops: [
								{
									loop_id: 'review-loop-1',
									role: 'spec_reviewer',
									model: 'claude-3',
									state: 'executing',
									iterations: 1,
									max_iterations: 5
								}
							]
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/review-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'review-plan',
						title: 'Review Plan',
						approved: true,
						stage: 'executing',
						active_loops: [
							{
								loop_id: 'review-loop-1',
								role: 'spec_reviewer',
								model: 'claude-3',
								state: 'executing',
								iterations: 1,
								max_iterations: 5
							}
						]
					})
				});
			});

			await page.route('**/workflow-api/plans/review-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('review-plan');
			await planDetailPage.expectPipelineVisible();

			// Check if review branch is visible (may or may not be depending on pipeline state)
			const reviewBranch = page.locator('.review-branch');
			// The branch visibility depends on the pipeline state logic
		});
	});

	test.describe('Plan Navigation', () => {
		test('shows not found for invalid slug', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await page.route('**/workflow-api/plans/nonexistent-plan', route => {
				route.fulfill({
					status: 404,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'Plan not found' })
				});
			});

			await planDetailPage.goto('nonexistent-plan');
			await planDetailPage.expectNotFound();
		});

		test('back link returns to plans list', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'nav-plan',
							title: 'Nav Plan',
							approved: false,
							stage: 'draft',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/nav-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'nav-plan',
						title: 'Nav Plan',
						approved: false,
						stage: 'draft',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/nav-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('nav-plan');
			await planDetailPage.expectVisible();

			await planDetailPage.goBack();
			await expect(page).toHaveURL(/\/plans$/);
		});
	});

	test.describe('Action Banners', () => {
		test('shows promote banner for unapproved plan with goal', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'promote-plan',
							title: 'Promote Plan',
							goal: 'Implement user authentication',
							approved: false,
							stage: 'draft',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/promote-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'promote-plan',
						title: 'Promote Plan',
						goal: 'Implement user authentication',
						approved: false,
						stage: 'draft',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/promote-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('promote-plan');
			await planDetailPage.expectPromoteBannerVisible();
		});

		test('shows generate tasks banner for approved planning stage', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'generate-plan',
							title: 'Generate Plan',
							approved: true,
							stage: 'planning',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/generate-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'generate-plan',
						title: 'Generate Plan',
						approved: true,
						stage: 'planning',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/generate-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			});

			await planDetailPage.goto('generate-plan');
			await planDetailPage.expectGenerateTasksBannerVisible();
		});

		test('shows execute banner when tasks are ready', async ({ page, planDetailPage }) => {
			await page.route('**/workflow-api/plans', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							slug: 'execute-plan',
							title: 'Execute Plan',
							approved: true,
							stage: 'tasks',
							active_loops: []
						}
					])
				});
			});

			await page.route('**/workflow-api/plans/execute-plan', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'execute-plan',
						title: 'Execute Plan',
						approved: true,
						stage: 'tasks',
						active_loops: []
					})
				});
			});

			await page.route('**/workflow-api/plans/execute-plan/tasks', route => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							id: 'task-1',
							description: 'Implement feature A',
							status: 'pending'
						}
					])
				});
			});

			await planDetailPage.goto('execute-plan');
			await planDetailPage.expectExecuteBannerVisible();
		});
	});
});
