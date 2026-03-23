import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for semspec-ui E2E tests.
 *
 * Two projects:
 * - "ui" — stateless tests (parallel): health, settings, plan-create, plan-list, etc.
 * - "cascade" — tests that trigger the mock LLM cascade (serial): plan-approve,
 *   plan-lifecycle, plan-rejection, plan-review. Serial because the mock LLM
 *   does not support parallel execution.
 *
 * The cascade project runs after the ui project completes.
 *
 * Timeout configuration:
 * - Default: 90s global, 22.5s per expect
 * - Override with PLAYWRIGHT_TIMEOUT env var for slow environments
 */

const DEFAULT_TIMEOUT = 90000;
const timeout = parseInt(process.env.PLAYWRIGHT_TIMEOUT || String(DEFAULT_TIMEOUT), 10);

const useMockLLM = process.env.USE_MOCK_LLM === 'true';
const dockerComposeCommand = useMockLLM
	? 'docker compose -f docker-compose.e2e.yml -f docker-compose.e2e-mock.yml up --wait'
	: 'docker compose -f docker-compose.e2e.yml up --wait';

// Specs that trigger the mock LLM cascade — must run serially
const CASCADE_SPECS = [
	'e2e/plan-approve.spec.ts',
	'e2e/plan-lifecycle.spec.ts',
	'e2e/plan-lifecycle-llm.spec.ts',
	'e2e/plan-rejection.spec.ts',
	'e2e/plan-review.spec.ts'
];

export default defineConfig({
	testDir: './e2e',
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: [
		['html', { outputFolder: 'playwright-report' }],
		['list']
	],
	use: {
		baseURL: 'http://localhost:3000',
		trace: 'on-first-retry',
		screenshot: 'only-on-failure',
		video: 'on-first-retry',
	},
	projects: [
		{
			name: 'ui',
			testIgnore: CASCADE_SPECS,
			use: { ...devices['Desktop Chrome'] },
			fullyParallel: true,
		},
		{
			name: 'cascade',
			testMatch: CASCADE_SPECS,
			use: { ...devices['Desktop Chrome'] },
			fullyParallel: false,
			dependencies: ['ui'],
		},
	],
	webServer: {
		command: dockerComposeCommand,
		url: 'http://localhost:3000',
		reuseExistingServer: !process.env.CI,
		timeout: 120 * 1000,
		stdout: 'pipe',
		stderr: 'pipe',
	},
	timeout,
	expect: {
		timeout: Math.round(timeout / 4),
	},
});
