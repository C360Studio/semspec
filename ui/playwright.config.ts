import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for semspec-ui E2E tests.
 *
 * Three projects:
 * - "t0" — stateless UI tests (parallel): health, settings, plan-create, plan-list, etc.
 * - "t1" — mock LLM journey tests (serial, one plan per journey): plan-journey,
 *   plan-rejection-journey. Serial because mock LLM fixtures are consumed sequentially.
 * - "t2" — real LLM journey tests (serial): plan-lifecycle-llm.
 *
 * t1 and t2 depend on t0 completing first.
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

const T1_SPECS = ['e2e/plan-journey.spec.ts', 'e2e/plan-rejection-journey.spec.ts'];
const T2_SPECS = ['e2e/plan-lifecycle-llm.spec.ts'];

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
			name: 't0',
			testIgnore: [...T1_SPECS, ...T2_SPECS],
			use: { ...devices['Desktop Chrome'] },
			fullyParallel: true,
		},
		{
			name: 't1',
			testMatch: T1_SPECS,
			use: { ...devices['Desktop Chrome'] },
			fullyParallel: false,
			dependencies: ['t0'],
		},
		{
			name: 't2',
			testMatch: T2_SPECS,
			use: { ...devices['Desktop Chrome'] },
			fullyParallel: false,
			dependencies: ['t0'],
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
