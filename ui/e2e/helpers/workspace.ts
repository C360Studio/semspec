/**
 * Workspace helpers for E2E tests.
 *
 * These functions manipulate the test workspace directory mounted into
 * the semspec container. Since the workspace is a shared volume, Playwright
 * can directly modify files on the host.
 *
 * IMPORTANT: Tests that modify workspace state must run serially to prevent
 * interference. Use test.describe.configure({ mode: 'serial' }) in test files.
 *
 * NOTE: The fixture directory contains files used by other tests (AST processors).
 * These helpers only manipulate files relevant to wizard testing and preserve
 * the rest of the fixture structure.
 */
import * as fs from 'fs/promises';
import * as path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';
import type { Page } from '@playwright/test';

// Get __dirname equivalent in ES modules
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to the test workspace mounted into the Docker container
const WORKSPACE = path.resolve(__dirname, '../../../test/e2e/fixtures/go-project');

// Sync marker file for verifying container sees filesystem changes
const SYNC_MARKER = '.e2e-sync-marker';

/**
 * Reset the workspace to a clean uninitialized state for wizard tests.
 * Only removes wizard-created files, preserves fixture structure.
 */
export async function resetWorkspace(): Promise<void> {
	// Remove wizard-created config files but preserve fixture's .semspec test data
	const wizardFiles = [
		'.semspec/project.json',
		'.semspec/checklist.json',
		'.semspec/standards.json'
	];

	for (const file of wizardFiles) {
		await fs.rm(path.join(WORKSPACE, file), { force: true });
	}
}

/**
 * Seed an empty project (no language marker files).
 * Simulates a greenfield project where detection finds nothing.
 * Preserves internal/ directory used by AST processor tests.
 */
export async function seedEmptyProject(): Promise<void> {
	await resetWorkspace();
	// Only remove language detection markers, not the full fixture
	await fs.rm(path.join(WORKSPACE, 'go.mod'), { force: true });
}

/**
 * Seed a Go project with marker files.
 * Detection will find Go language and tooling.
 */
export async function seedGoProject(): Promise<void> {
	await resetWorkspace();
	// Ensure go.mod exists for language detection
	const goModContent = `module example.com/test

go 1.21
`;
	await fs.writeFile(path.join(WORKSPACE, 'go.mod'), goModContent);
}

/**
 * Seed an initialized project (has .semspec/ config files).
 * The wizard should show as complete.
 */
export async function seedInitializedProject(): Promise<void> {
	await seedGoProject();

	const semspecDir = path.join(WORKSPACE, '.semspec');
	await fs.mkdir(semspecDir, { recursive: true });

	// Create minimal config files
	await fs.writeFile(
		path.join(semspecDir, 'project.json'),
		JSON.stringify(
			{
				name: 'test-project',
				description: 'Test project',
				languages: ['Go'],
				frameworks: []
			},
			null,
			2
		)
	);

	await fs.writeFile(
		path.join(semspecDir, 'checklist.json'),
		JSON.stringify(
			{
				checks: [
					{
						name: 'go-build',
						command: 'go build ./...',
						category: 'compile',
						required: true
					}
				]
			},
			null,
			2
		)
	);

	await fs.writeFile(
		path.join(semspecDir, 'standards.json'),
		JSON.stringify(
			{
				version: '1.0.0',
				rules: []
			},
			null,
			2
		)
	);
}

/**
 * Restore the workspace to its original committed state.
 * Removes wizard-created files and uses git checkout to restore tracked files.
 * Call this in afterAll to clean up.
 */
export async function restoreWorkspace(): Promise<void> {
	// Remove wizard-created files that aren't tracked in git
	const wizardFiles = [
		'.semspec/project.json',
		'.semspec/checklist.json',
		'.semspec/standards.json',
		'.semspec/scaffold.json'
	];

	for (const file of wizardFiles) {
		await fs.rm(path.join(WORKSPACE, file), { force: true });
	}

	try {
		// Use git to restore tracked files to their committed state
		execSync('git checkout HEAD -- .', { cwd: WORKSPACE, stdio: 'ignore' });
	} catch {
		// Fallback: just ensure go.mod exists for basic functionality
		await seedGoProject();
	}
}

/**
 * Write a unique sync marker file to the workspace.
 * Used to verify Docker container sees filesystem changes.
 */
async function writeSyncMarker(): Promise<string> {
	const marker = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
	await fs.writeFile(path.join(WORKSPACE, SYNC_MARKER), marker);
	return marker;
}

/**
 * Remove the sync marker file.
 */
async function removeSyncMarker(): Promise<void> {
	await fs.rm(path.join(WORKSPACE, SYNC_MARKER), { force: true });
}

/**
 * Wait for the backend to see the expected workspace state.
 * Polls /project-api/detect until it returns the expected result.
 *
 * @param page - Playwright page for making API requests
 * @param expectEmpty - If true, expects no languages detected (greenfield)
 * @param maxAttempts - Maximum poll attempts (default 10)
 * @param delayMs - Delay between attempts (default 100ms)
 */
export async function waitForWorkspaceSync(
	page: Page,
	expectEmpty: boolean,
	maxAttempts = 10,
	delayMs = 100
): Promise<void> {
	for (let attempt = 0; attempt < maxAttempts; attempt++) {
		try {
			const response = await page.request.post('/project-api/detect');
			if (response.ok()) {
				const data = await response.json();
				const hasLanguages = data.languages && data.languages.length > 0;

				if (expectEmpty && !hasLanguages) {
					// Backend sees empty workspace as expected
					return;
				}
				if (!expectEmpty && hasLanguages) {
					// Backend sees project files as expected
					return;
				}
			}
		} catch {
			// Request failed, will retry
		}

		// Wait before next attempt
		await new Promise((resolve) => setTimeout(resolve, delayMs));
	}

	throw new Error(
		`Workspace sync failed after ${maxAttempts} attempts. ` +
			`Expected ${expectEmpty ? 'empty' : 'populated'} workspace.`
	);
}

/**
 * Seed an empty project and wait for backend to see it.
 * Use this in tests that need guaranteed empty state.
 */
export async function seedEmptyProjectAndSync(page: Page): Promise<void> {
	await seedEmptyProject();
	await waitForWorkspaceSync(page, true);
}

/**
 * Seed a Go project and wait for backend to see it.
 * Use this in tests that need guaranteed Go project state.
 */
export async function seedGoProjectAndSync(page: Page): Promise<void> {
	await seedGoProject();
	await waitForWorkspaceSync(page, false);
}
