/**
 * Tests for the SetupStore.
 *
 * The store orchestrates the multi-step setup wizard. These tests verify:
 * - State transitions between wizard steps
 * - Mutation methods for checklist and rules
 * - Derived property logic
 * - Reset behavior
 *
 * Because @testing-library/svelte is not available, and the test environment
 * is node (no DOM), we test the store's pure logic directly: instantiating it,
 * calling methods, and inspecting state. API calls are not tested here —
 * integration is covered by E2E tests.
 *
 * Note: Svelte 5 runes ($state) are compile-time transforms. The compiled
 * output exposes plain property access, so the store is fully testable as
 * a regular class in node environment.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';

// ---------------------------------------------------------------------------
// Helpers — build fixture data without importing from API module to avoid
// circular resolution issues in the node test environment.
// ---------------------------------------------------------------------------

import type { Check, Rule, InitStatus, DetectionResult } from '$lib/api/project';

function makeCheck(overrides: Partial<Check> = {}): Check {
	return {
		name: 'Test Check',
		command: 'go test ./...',
		trigger: [],
		category: 'test',
		required: true,
		timeout: '60s',
		description: 'Run tests',
		...overrides
	};
}

function makeRule(overrides: Partial<Rule> = {}): Rule {
	return {
		id: 'rule-1',
		text: 'Always handle errors',
		severity: 'error',
		category: 'error-handling',
		origin: 'CLAUDE.md',
		...overrides
	};
}

function makeStatus(overrides: Partial<InitStatus> = {}): InitStatus {
	return {
		initialized: false,
		has_project_json: false,
		has_checklist: false,
		has_standards: false,
		sop_count: 0,
		workspace_path: '/workspace/my-project',
		...overrides
	};
}

function makeDetection(overrides: Partial<DetectionResult> = {}): DetectionResult {
	return {
		languages: [{ name: 'Go', version: '1.23', marker: 'go.mod', confidence: 'high', primary: true }],
		frameworks: [],
		tooling: [],
		existing_docs: [],
		proposed_checklist: [makeCheck({ name: 'Go Build', command: 'go build ./...' })],
		...overrides
	};
}

// ---------------------------------------------------------------------------
// Store module — import after defining fixtures
// ---------------------------------------------------------------------------

// We need to mock the API module before importing the store, since the store
// imports from $lib/api/project which makes real HTTP calls.
vi.mock('$lib/api/project', () => ({
	getStatus: vi.fn(),
	detect: vi.fn(),
	generateStandards: vi.fn(),
	initProject: vi.fn()
}));

import * as projectApi from '$lib/api/project';

// Import after mocking to get the real store class under test.
// We can't use the singleton `setupStore` export because state persists
// between tests — instead we re-import and reset.
// In Svelte 5, compiled runes are properties, so we import the module
// and test against the exported singleton by resetting between tests.
import { setupStore } from '$lib/stores/setup.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SetupStore — initial state', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('starts in loading step', () => {
		expect(setupStore.step).toBe('loading');
	});

	it('has null error initially', () => {
		expect(setupStore.error).toBeNull();
	});

	it('has null status initially', () => {
		expect(setupStore.status).toBeNull();
	});

	it('has null detection initially', () => {
		expect(setupStore.detection).toBeNull();
	});

	it('has empty project name and description', () => {
		expect(setupStore.projectName).toBe('');
		expect(setupStore.projectDescription).toBe('');
	});

	it('has empty checklist and rules', () => {
		expect(setupStore.checklist).toHaveLength(0);
		expect(setupStore.rules).toHaveLength(0);
	});

	it('has empty filesWritten', () => {
		expect(setupStore.filesWritten).toHaveLength(0);
	});
});

// ---------------------------------------------------------------------------
// Derived properties
// ---------------------------------------------------------------------------

describe('SetupStore — isInitialized', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('returns false when status is null', () => {
		expect(setupStore.isInitialized).toBe(false);
	});

	it('returns false when status.initialized is false', () => {
		setupStore.status = makeStatus({ initialized: false });
		expect(setupStore.isInitialized).toBe(false);
	});

	it('returns true when status.initialized is true', () => {
		setupStore.status = makeStatus({ initialized: true });
		expect(setupStore.isInitialized).toBe(true);
	});
});

describe('SetupStore — primaryLanguage', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('returns null when detection is null', () => {
		expect(setupStore.primaryLanguage).toBeNull();
	});

	it('returns the language marked as primary', () => {
		setupStore.detection = makeDetection({
			languages: [
				{ name: 'TypeScript', version: null, marker: 'tsconfig.json', confidence: 'high' },
				{ name: 'Go', version: '1.23', marker: 'go.mod', confidence: 'high', primary: true }
			]
		});

		expect(setupStore.primaryLanguage).toBe('Go');
	});

	it('falls back to the first language when none is primary', () => {
		setupStore.detection = makeDetection({
			languages: [
				{ name: 'TypeScript', version: null, marker: 'tsconfig.json', confidence: 'high' },
				{ name: 'Go', version: '1.23', marker: 'go.mod', confidence: 'high' }
			]
		});

		expect(setupStore.primaryLanguage).toBe('TypeScript');
	});

	it('returns null when languages array is empty', () => {
		setupStore.detection = makeDetection({ languages: [] });

		expect(setupStore.primaryLanguage).toBeNull();
	});
});

// ---------------------------------------------------------------------------
// checkStatus
// ---------------------------------------------------------------------------

describe('SetupStore — checkStatus', () => {
	beforeEach(() => {
		setupStore.reset();
		vi.clearAllMocks();
	});

	it('transitions to complete step when project is already initialized', async () => {
		vi.mocked(projectApi.getStatus).mockResolvedValue(makeStatus({ initialized: true }));

		await setupStore.checkStatus();

		expect(setupStore.step).toBe('complete');
		expect(setupStore.status?.initialized).toBe(true);
	});

	it('runs detection when project is not initialized', async () => {
		vi.mocked(projectApi.getStatus).mockResolvedValue(makeStatus({ initialized: false }));
		vi.mocked(projectApi.detect).mockResolvedValue(makeDetection());

		await setupStore.checkStatus();

		expect(projectApi.detect).toHaveBeenCalledOnce();
		expect(setupStore.step).toBe('detection');
	});

	it('transitions to error step on API failure', async () => {
		vi.mocked(projectApi.getStatus).mockRejectedValue(new Error('Network error'));

		await setupStore.checkStatus();

		expect(setupStore.step).toBe('error');
		expect(setupStore.error).toBe('Network error');
	});

	it('clears previous error on retry', async () => {
		// First call fails
		vi.mocked(projectApi.getStatus).mockRejectedValue(new Error('First error'));
		await setupStore.checkStatus();
		expect(setupStore.error).toBe('First error');

		// Second call succeeds
		vi.mocked(projectApi.getStatus).mockResolvedValue(makeStatus({ initialized: true }));
		await setupStore.checkStatus();

		expect(setupStore.error).toBeNull();
		expect(setupStore.step).toBe('complete');
	});
});

// ---------------------------------------------------------------------------
// runDetection
// ---------------------------------------------------------------------------

describe('SetupStore — runDetection', () => {
	beforeEach(() => {
		setupStore.reset();
		vi.clearAllMocks();
	});

	it('sets step to detecting during fetch then detection after success', async () => {
		vi.mocked(projectApi.detect).mockResolvedValue(makeDetection());

		await setupStore.runDetection();

		expect(setupStore.step).toBe('detection');
	});

	it('copies proposed_checklist to editable checklist', async () => {
		const detection = makeDetection({
			proposed_checklist: [
				makeCheck({ name: 'Build', command: 'go build ./...' }),
				makeCheck({ name: 'Test', command: 'go test ./...' })
			]
		});
		vi.mocked(projectApi.detect).mockResolvedValue(detection);

		await setupStore.runDetection();

		expect(setupStore.checklist).toHaveLength(2);
		expect(setupStore.checklist[0].name).toBe('Build');
		expect(setupStore.checklist[1].name).toBe('Test');
	});

	it('auto-suggests project name from workspace_path tail segment', async () => {
		setupStore.status = makeStatus({ workspace_path: '/home/user/my-awesome-project' });
		vi.mocked(projectApi.detect).mockResolvedValue(makeDetection());

		await setupStore.runDetection();

		expect(setupStore.projectName).toBe('my-awesome-project');
	});

	it('does not override a user-entered project name', async () => {
		setupStore.status = makeStatus({ workspace_path: '/home/user/repo' });
		setupStore.projectName = 'custom-name';
		vi.mocked(projectApi.detect).mockResolvedValue(makeDetection());

		await setupStore.runDetection();

		expect(setupStore.projectName).toBe('custom-name');
	});

	it('sets error step on failure', async () => {
		vi.mocked(projectApi.detect).mockRejectedValue(new Error('Detection failed'));

		await setupStore.runDetection();

		expect(setupStore.step).toBe('error');
		expect(setupStore.error).toBe('Detection failed');
	});

	it('handles non-Error exceptions with fallback message', async () => {
		vi.mocked(projectApi.detect).mockRejectedValue('raw string error');

		await setupStore.runDetection();

		expect(setupStore.step).toBe('error');
		expect(setupStore.error).toBe('Detection failed');
	});
});

// ---------------------------------------------------------------------------
// proceedToChecklist and proceedToStandards
// ---------------------------------------------------------------------------

describe('SetupStore — step navigation', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('proceedToChecklist sets step to checklist', () => {
		setupStore.step = 'detection';
		setupStore.proceedToChecklist();

		expect(setupStore.step).toBe('checklist');
	});

	it('proceedToStandards sets step to standards', () => {
		setupStore.step = 'checklist';
		setupStore.proceedToStandards();

		expect(setupStore.step).toBe('standards');
	});

	it('goBack from checklist returns to detection', () => {
		setupStore.step = 'checklist';
		setupStore.goBack();

		expect(setupStore.step).toBe('detection');
	});

	it('goBack from standards returns to checklist', () => {
		setupStore.step = 'standards';
		setupStore.goBack();

		expect(setupStore.step).toBe('checklist');
	});

	it('goBack from detection does nothing', () => {
		setupStore.step = 'detection';
		setupStore.goBack();

		expect(setupStore.step).toBe('detection');
	});
});

// ---------------------------------------------------------------------------
// generateStandards
// ---------------------------------------------------------------------------

describe('SetupStore — generateStandards', () => {
	beforeEach(() => {
		setupStore.reset();
		vi.clearAllMocks();
	});

	it('does nothing when detection is null', async () => {
		await setupStore.generateStandards();

		expect(projectApi.generateStandards).not.toHaveBeenCalled();
	});

	it('populates rules from API response', async () => {
		setupStore.detection = makeDetection();
		vi.mocked(projectApi.generateStandards).mockResolvedValue({
			rules: [
				makeRule({ id: 'r1', text: 'Handle errors' }),
				makeRule({ id: 'r2', text: 'Use context' })
			],
			token_estimate: 800
		});

		await setupStore.generateStandards();

		expect(setupStore.rules).toHaveLength(2);
		expect(setupStore.rules[0].id).toBe('r1');
	});

	it('fails gracefully when API throws — rules stay empty', async () => {
		setupStore.detection = makeDetection();
		vi.mocked(projectApi.generateStandards).mockRejectedValue(new Error('LLM unavailable'));

		await setupStore.generateStandards();

		// Should not throw; rules remain empty
		expect(setupStore.rules).toHaveLength(0);
		expect(setupStore.step).not.toBe('error');
	});
});

// ---------------------------------------------------------------------------
// Checklist mutations
// ---------------------------------------------------------------------------

describe('SetupStore — checklist mutations', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('addCheck appends to checklist', () => {
		setupStore.addCheck(makeCheck({ name: 'Alpha' }));
		setupStore.addCheck(makeCheck({ name: 'Beta' }));

		expect(setupStore.checklist).toHaveLength(2);
		expect(setupStore.checklist[1].name).toBe('Beta');
	});

	it('removeCheck removes by index', () => {
		setupStore.addCheck(makeCheck({ name: 'A' }));
		setupStore.addCheck(makeCheck({ name: 'B' }));
		setupStore.addCheck(makeCheck({ name: 'C' }));

		setupStore.removeCheck(1);

		expect(setupStore.checklist).toHaveLength(2);
		expect(setupStore.checklist[0].name).toBe('A');
		expect(setupStore.checklist[1].name).toBe('C');
	});

	it('updateCheck replaces a check at given index', () => {
		setupStore.addCheck(makeCheck({ name: 'Original' }));
		setupStore.updateCheck(0, makeCheck({ name: 'Updated', command: 'make test' }));

		expect(setupStore.checklist[0].name).toBe('Updated');
		expect(setupStore.checklist[0].command).toBe('make test');
	});

	it('toggleCheckRequired flips the required flag', () => {
		setupStore.addCheck(makeCheck({ required: true }));
		setupStore.toggleCheckRequired(0);

		expect(setupStore.checklist[0].required).toBe(false);

		setupStore.toggleCheckRequired(0);

		expect(setupStore.checklist[0].required).toBe(true);
	});

	it('mutations do not mutate detection.proposed_checklist', async () => {
		const detection = makeDetection({
			proposed_checklist: [makeCheck({ name: 'Original' })]
		});
		vi.mocked(projectApi.detect).mockResolvedValue(detection);
		await setupStore.runDetection();

		setupStore.removeCheck(0);

		// The detection result's proposed_checklist is unaffected
		expect(setupStore.detection?.proposed_checklist).toHaveLength(1);
		expect(setupStore.checklist).toHaveLength(0);
	});
});

// ---------------------------------------------------------------------------
// Rule mutations
// ---------------------------------------------------------------------------

describe('SetupStore — rule mutations', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('addRule appends to rules', () => {
		setupStore.addRule(makeRule({ id: 'r1' }));
		setupStore.addRule(makeRule({ id: 'r2' }));

		expect(setupStore.rules).toHaveLength(2);
		expect(setupStore.rules[1].id).toBe('r2');
	});

	it('removeRule removes by index', () => {
		setupStore.addRule(makeRule({ id: 'r1' }));
		setupStore.addRule(makeRule({ id: 'r2' }));
		setupStore.addRule(makeRule({ id: 'r3' }));

		setupStore.removeRule(1);

		expect(setupStore.rules).toHaveLength(2);
		expect(setupStore.rules[0].id).toBe('r1');
		expect(setupStore.rules[1].id).toBe('r3');
	});

	it('updateRule replaces a rule at given index', () => {
		setupStore.addRule(makeRule({ id: 'r1', text: 'Original rule' }));
		setupStore.updateRule(0, makeRule({ id: 'r1', text: 'Updated rule' }));

		expect(setupStore.rules[0].text).toBe('Updated rule');
	});
});

// ---------------------------------------------------------------------------
// initialize
// ---------------------------------------------------------------------------

describe('SetupStore — initialize', () => {
	beforeEach(() => {
		setupStore.reset();
		vi.clearAllMocks();
	});

	it('does nothing when detection is null', async () => {
		await setupStore.initialize();

		expect(projectApi.initProject).not.toHaveBeenCalled();
		expect(setupStore.step).toBe('loading');
	});

	it('transitions to complete step on success', async () => {
		setupStore.detection = makeDetection();
		setupStore.projectName = 'semspec';
		vi.mocked(projectApi.initProject).mockResolvedValue({
			success: true,
			files_written: ['.semspec/project.json', '.semspec/checklist.yaml']
		});

		await setupStore.initialize();

		expect(setupStore.step).toBe('complete');
		expect(setupStore.filesWritten).toHaveLength(2);
		expect(setupStore.filesWritten[0]).toBe('.semspec/project.json');
	});

	it('sends correct payload to initProject', async () => {
		setupStore.detection = makeDetection({
			languages: [
				{ name: 'Go', version: '1.23', marker: 'go.mod', confidence: 'high', primary: true },
				{ name: 'TypeScript', version: null, marker: 'tsconfig.json', confidence: 'high' }
			],
			frameworks: [
				{ name: 'SvelteKit', language: 'TypeScript', marker: 'svelte.config.js', confidence: 'high' }
			]
		});
		setupStore.projectName = 'my-project';
		setupStore.projectDescription = 'A test project';
		setupStore.checklist = [makeCheck({ name: 'Lint' })];
		setupStore.rules = [makeRule({ id: 'r1' })];

		vi.mocked(projectApi.initProject).mockResolvedValue({
			success: true,
			files_written: []
		});

		await setupStore.initialize();

		const call = vi.mocked(projectApi.initProject).mock.calls[0][0];
		expect(call.project.name).toBe('my-project');
		expect(call.project.description).toBe('A test project');
		expect(call.project.languages).toEqual(['Go', 'TypeScript']);
		expect(call.project.frameworks).toEqual(['SvelteKit']);
		expect(call.checklist).toHaveLength(1);
		expect(call.standards.version).toBe('1.0.0');
		expect(call.standards.rules).toHaveLength(1);
	});

	it('sends undefined description when description is empty', async () => {
		setupStore.detection = makeDetection();
		setupStore.projectName = 'test';
		setupStore.projectDescription = '';

		vi.mocked(projectApi.initProject).mockResolvedValue({
			success: true,
			files_written: []
		});

		await setupStore.initialize();

		const call = vi.mocked(projectApi.initProject).mock.calls[0][0];
		expect(call.project.description).toBeUndefined();
	});

	it('transitions to error step on API failure', async () => {
		setupStore.detection = makeDetection();
		setupStore.projectName = 'test';
		vi.mocked(projectApi.initProject).mockRejectedValue(new Error('Write failed'));

		await setupStore.initialize();

		expect(setupStore.step).toBe('error');
		expect(setupStore.error).toBe('Write failed');
	});
});

// ---------------------------------------------------------------------------
// reset
// ---------------------------------------------------------------------------

describe('SetupStore — reset', () => {
	beforeEach(() => {
		setupStore.reset();
	});

	it('restores all fields to initial values', () => {
		// Pollute state
		setupStore.step = 'checklist';
		setupStore.error = 'Something went wrong';
		setupStore.status = makeStatus({ initialized: true });
		setupStore.detection = makeDetection();
		setupStore.projectName = 'my-project';
		setupStore.projectDescription = 'A description';
		setupStore.checklist = [makeCheck()];
		setupStore.rules = [makeRule()];
		setupStore.filesWritten = ['file.json'];

		setupStore.reset();

		expect(setupStore.step).toBe('loading');
		expect(setupStore.error).toBeNull();
		expect(setupStore.status).toBeNull();
		expect(setupStore.detection).toBeNull();
		expect(setupStore.projectName).toBe('');
		expect(setupStore.projectDescription).toBe('');
		expect(setupStore.checklist).toHaveLength(0);
		expect(setupStore.rules).toHaveLength(0);
		expect(setupStore.filesWritten).toHaveLength(0);
	});
});
