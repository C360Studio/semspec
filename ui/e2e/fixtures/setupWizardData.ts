/**
 * Mock data fixtures for Setup Wizard E2E tests.
 *
 * NOTE: Most tests use real backend infrastructure via workspace seeding.
 * These mocks are only used for:
 * - Error condition tests (500 responses)
 * - Loading state accessibility tests (never-resolving requests)
 * - Plans nudge tests (specific data requirements)
 */

/**
 * Empty detection result - used when mocking greenfield projects
 * in error condition tests.
 */
export const mockEmptyDetection = {
	languages: [],
	frameworks: [],
	tooling: [],
	existing_docs: [],
	proposed_checklist: []
};

/**
 * Wizard options - kept for reference but real backend is preferred.
 */
export const mockWizardOptions = {
	languages: [
		{ name: 'Go', marker: 'go.mod', has_ast: true },
		{ name: 'Python', marker: 'requirements.txt', has_ast: true },
		{ name: 'TypeScript', marker: 'tsconfig.json', has_ast: true },
		{ name: 'Svelte', marker: 'svelte.config.js', has_ast: false }
	],
	frameworks: [
		{ name: 'Flask', language: 'Python' },
		{ name: 'FastAPI', language: 'Python' },
		{ name: 'SvelteKit', language: 'Svelte' },
		{ name: 'Gin', language: 'Go' }
	]
};

/**
 * Uninitialized project status - used for loading state tests
 * where we need to control the status response timing.
 */
export const mockUninitializedStatus = {
	initialized: false,
	has_project_json: false,
	has_checklist: false,
	has_standards: false,
	sop_count: 0,
	workspace_path: '/test/project'
};
