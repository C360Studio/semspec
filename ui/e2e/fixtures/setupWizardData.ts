/**
 * Mock data fixtures for Setup Wizard E2E tests.
 */

export const mockEmptyDetection = {
	languages: [],
	frameworks: [],
	tooling: [],
	existing_docs: [],
	proposed_checklist: []
};

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

export const mockScaffoldResponse = {
	files_created: ['go.mod', 'main.go'],
	semspec_dir: '.semspec'
};

export const mockGoDetection = {
	languages: [{ name: 'Go', version: '1.21', marker: 'go.mod', confidence: 'high', primary: true }],
	frameworks: [{ name: 'Gin', version: '1.9', marker: 'go.mod', confidence: 'medium' }],
	tooling: [{ name: 'golangci-lint', config_file: '.golangci.yml' }],
	existing_docs: [{ path: 'README.md', type: 'readme' }],
	proposed_checklist: [
		{ name: 'go-vet', command: 'go vet ./...', required: true },
		{ name: 'go-test', command: 'go test ./...', required: true }
	]
};

export const mockInitializedStatus = {
	initialized: true,
	has_project_json: true,
	has_checklist: true,
	has_standards: true,
	sop_count: 2,
	workspace_path: '/test/project'
};

export const mockUninitializedStatus = {
	initialized: false,
	has_project_json: false,
	has_checklist: false,
	has_standards: false,
	sop_count: 0,
	workspace_path: '/test/project'
};

export const mockInitResponse = {
	success: true,
	files_written: ['.semspec/project.yaml', '.semspec/checklist.yaml', '.semspec/standards.yaml']
};

export const mockGenerateStandardsResponse = {
	rules: [],
	token_estimate: 0
};
