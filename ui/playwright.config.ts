import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for semspec-ui E2E tests.
 *
 * Tests run against the Docker Compose E2E stack which includes:
 * - NATS JetStream (messaging)
 * - semspec backend (API)
 * - UI dev server (Vite)
 * - Caddy (reverse proxy)
 *
 * Timeout configuration:
 * - Default: 90s global, 22.5s per expect
 * - Override with PLAYWRIGHT_TIMEOUT env var for slow environments
 * - Example: PLAYWRIGHT_TIMEOUT=120000 npm run test:e2e
 */

const DEFAULT_TIMEOUT = 90000;
const timeout = parseInt(process.env.PLAYWRIGHT_TIMEOUT || String(DEFAULT_TIMEOUT), 10);

export default defineConfig({
	testDir: './e2e',
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: process.env.CI ? 1 : undefined,
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
			name: 'chromium',
			use: { ...devices['Desktop Chrome'] },
		},
	],
	webServer: {
		command: 'docker compose -f docker-compose.e2e.yml up --wait',
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
