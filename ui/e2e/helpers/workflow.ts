import type { Page } from '@playwright/test';

/**
 * Helper functions for triggering and managing workflows in E2E tests.
 */

export interface Loop {
	loop_id: string;
	task_id: string;
	user_id: string;
	channel_type: string;
	channel_id: string;
	state: string;
	iterations: number;
	max_iterations: number;
	created_at: string;
	workflow_slug?: string;
	workflow_step?: string;
	role?: string;
	model?: string;
	context_request_id?: string;
}

export interface Plan {
	slug: string;
	title?: string;
	goal?: string;
	approved: boolean;
	stage: string;
	active_loops: ActiveLoop[];
	github?: {
		epic_number: number;
		epic_url: string;
	};
}

export interface ActiveLoop {
	loop_id: string;
	role: string;
	model: string;
	state: string;
	iterations: number;
	max_iterations: number;
}

export interface ReviewResult {
	verdict: string;
	findings: Finding[];
	reviewers: ReviewerSummary[];
	stats: ReviewStats;
	summary?: string;
	partial?: boolean;
	missing_reviewers?: string[];
}

export interface Finding {
	role: string;
	category: string;
	severity: string;
	file: string;
	line: number;
	issue: string;
	suggestion: string;
	sop_id?: string;
	status?: string;
	cwe?: string;
}

export interface ReviewerSummary {
	role: string;
	verdict: string;
	passed: boolean;
	summary?: string;
}

export interface ReviewStats {
	total_findings: number;
	by_severity: Record<string, number>;
	by_reviewer: Record<string, number>;
	reviewers_total: number;
	reviewers_passed: number;
}

/**
 * Get the base URL for API requests
 */
function getBaseURL(page: Page): string {
	// Use the page's base URL which points to the Caddy proxy
	return 'http://localhost:3000';
}

/**
 * Send a chat message via the API
 */
export async function sendMessage(page: Page, content: string): Promise<{ message_id?: string; error?: string }> {
	const baseURL = getBaseURL(page);
	const response = await page.request.post(`${baseURL}/agentic-dispatch/message`, {
		data: { content }
	});

	if (!response.ok()) {
		return { error: `Failed to send message: ${response.status()}` };
	}

	return await response.json();
}

/**
 * Trigger a workflow by sending a /propose command
 */
export async function triggerWorkflow(page: Page, description: string): Promise<string | null> {
	const result = await sendMessage(page, `/propose ${description}`);
	if (result.error) {
		console.error('Failed to trigger workflow:', result.error);
		return null;
	}
	// Return a slug derived from the description
	return description.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
}

/**
 * Get all plans from the API
 */
export async function getPlans(page: Page): Promise<Plan[]> {
	const baseURL = getBaseURL(page);
	const response = await page.request.get(`${baseURL}/workflow-api/plans`);

	if (!response.ok()) {
		console.error('Failed to fetch plans:', response.status());
		return [];
	}

	return await response.json();
}

/**
 * Get a specific plan by slug
 */
export async function getPlan(page: Page, slug: string): Promise<Plan | null> {
	const baseURL = getBaseURL(page);
	const response = await page.request.get(`${baseURL}/workflow-api/plans/${slug}`);

	if (!response.ok()) {
		return null;
	}

	return await response.json();
}

/**
 * Get reviews for a plan
 */
export async function getReviews(page: Page, slug: string): Promise<ReviewResult | null> {
	const baseURL = getBaseURL(page);
	const response = await page.request.get(`${baseURL}/workflow-api/plans/${slug}/reviews`);

	if (!response.ok()) {
		return null;
	}

	return await response.json();
}

/**
 * Wait for a plan to exist
 */
export async function waitForPlan(
	page: Page,
	slug: string,
	options: { timeout?: number; interval?: number } = {}
): Promise<Plan | null> {
	const { timeout = 30000, interval = 1000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const plan = await getPlan(page, slug);
		if (plan) {
			return plan;
		}
		await page.waitForTimeout(interval);
	}

	return null;
}

/**
 * Wait for reviews to be available for a plan
 */
export async function waitForReviewComplete(
	page: Page,
	slug: string,
	options: { timeout?: number; interval?: number } = {}
): Promise<ReviewResult | null> {
	const { timeout = 60000, interval = 2000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const reviews = await getReviews(page, slug);
		if (reviews && reviews.reviewers.length > 0) {
			return reviews;
		}
		await page.waitForTimeout(interval);
	}

	return null;
}

/**
 * Wait for a plan to reach a specific stage
 */
export async function waitForPlanStage(
	page: Page,
	slug: string,
	stage: string,
	options: { timeout?: number; interval?: number } = {}
): Promise<Plan | null> {
	const { timeout = 60000, interval = 2000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const plan = await getPlan(page, slug);
		if (plan && plan.stage === stage) {
			return plan;
		}
		await page.waitForTimeout(interval);
	}

	return null;
}

/**
 * Wait for a plan to reach any of the specified stages.
 * Useful when the mock LLM progresses faster than polling.
 */
export async function waitForPlanStageOneOf(
	page: Page,
	slug: string,
	stages: string[],
	options: { timeout?: number; interval?: number } = {}
): Promise<Plan | null> {
	const { timeout = 60000, interval = 2000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const plan = await getPlan(page, slug);
		if (plan && stages.includes(plan.stage)) {
			return plan;
		}
		await page.waitForTimeout(interval);
	}

	return null;
}

/**
 * Get all active loops from the API
 */
export async function getActiveLoops(page: Page): Promise<Loop[]> {
	const baseURL = getBaseURL(page);
	const response = await page.request.get(`${baseURL}/agentic-dispatch/loops`);

	if (!response.ok()) {
		console.error('Failed to fetch loops:', response.status());
		return [];
	}

	return await response.json();
}

/**
 * Wait for active loops to exist
 */
export async function waitForActiveLoops(
	page: Page,
	options: { minCount?: number; timeout?: number; interval?: number } = {}
): Promise<Loop[]> {
	const { minCount = 1, timeout = 30000, interval = 1000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const loops = await getActiveLoops(page);
		if (loops.length >= minCount) {
			return loops;
		}
		await page.waitForTimeout(interval);
	}

	return [];
}

/**
 * Get a loop with a context_request_id for context panel testing
 */
export async function getLoopWithContext(page: Page): Promise<Loop | null> {
	const loops = await getActiveLoops(page);
	return loops.find((l) => l.context_request_id) || null;
}

/**
 * Wait for a loop with context_request_id
 */
export async function waitForLoopWithContext(
	page: Page,
	options: { timeout?: number; interval?: number } = {}
): Promise<Loop | null> {
	const { timeout = 30000, interval = 1000 } = options;
	const start = Date.now();

	while (Date.now() - start < timeout) {
		const loop = await getLoopWithContext(page);
		if (loop) {
			return loop;
		}
		await page.waitForTimeout(interval);
	}

	return null;
}

/**
 * Get context build response for a context request ID
 */
export async function getContextResponse(page: Page, requestId: string): Promise<unknown> {
	const baseURL = getBaseURL(page);
	const response = await page.request.get(`${baseURL}/context-builder/responses/${requestId}`);

	if (!response.ok()) {
		return null;
	}

	return await response.json();
}

/**
 * Create mock loop data for testing without real backend
 */
export function createMockLoop(overrides: Partial<Loop> = {}): Loop {
	const id = `loop-${Math.random().toString(36).slice(2, 10)}`;
	return {
		loop_id: id,
		task_id: `task-${id}`,
		user_id: 'test-user',
		channel_type: 'http',
		channel_id: 'test-channel',
		state: 'executing',
		iterations: 1,
		max_iterations: 10,
		created_at: new Date().toISOString(),
		...overrides
	};
}

/**
 * Create mock loop data with context for testing
 */
export function createMockLoopWithContext(overrides: Partial<Loop> = {}): Loop {
	return createMockLoop({
		context_request_id: `ctx-${Math.random().toString(36).slice(2, 10)}`,
		...overrides
	});
}

/**
 * Create mock plan data for testing
 */
export function createMockPlan(overrides: Partial<Plan> = {}): Plan {
	const slug = overrides.slug || `test-plan-${Date.now()}`;
	return {
		slug,
		title: slug.replace(/-/g, ' '),
		approved: false,
		stage: 'exploration',
		active_loops: [],
		...overrides
	};
}

/**
 * Create mock review result data for testing
 */
export function createMockReviewResult(overrides: Partial<ReviewResult> = {}): ReviewResult {
	return {
		verdict: 'passed',
		findings: [],
		reviewers: [
			{ role: 'spec_reviewer', verdict: 'compliant', passed: true, summary: 'All specs compliant' },
			{ role: 'sop_reviewer', verdict: 'approved', passed: true, summary: 'Follows SOP guidelines' },
			{ role: 'style_reviewer', verdict: 'approved', passed: true, summary: 'Style is consistent' },
			{ role: 'security_reviewer', verdict: 'approved', passed: true, summary: 'No security issues' }
		],
		stats: {
			total_findings: 0,
			by_severity: {},
			by_reviewer: {},
			reviewers_total: 4,
			reviewers_passed: 4
		},
		...overrides
	};
}

/**
 * Create mock finding data for testing
 */
export function createMockFinding(overrides: Partial<Finding> = {}): Finding {
	return {
		role: 'style_reviewer',
		category: 'style',
		severity: 'medium',
		file: 'src/test.ts',
		line: 42,
		issue: 'Test finding issue',
		suggestion: 'Consider fixing this',
		...overrides
	};
}
