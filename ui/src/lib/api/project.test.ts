/**
 * Tests for the project API module.
 *
 * These tests verify the type contracts and API function signatures.
 * They run in node environment (no DOM) using Vitest, matching the
 * existing test patterns in this codebase.
 *
 * Integration with real endpoints is covered by E2E tests.
 */
import { describe, it, expect } from 'vitest';
import type {
	InitStatus,
	DetectedLanguage,
	DetectedFramework,
	DetectedTool,
	DetectedDoc,
	Check,
	DetectionResult,
	Rule,
	GenerateStandardsResponse,
	ProjectInitInput,
	InitRequest,
	InitResponse
} from './project';

// ---------------------------------------------------------------------------
// InitStatus
// ---------------------------------------------------------------------------

describe('InitStatus', () => {
	it('has all required fields', () => {
		const status: InitStatus = {
			initialized: false,
			has_project_json: false,
			has_checklist: false,
			has_standards: false,
			sop_count: 0,
			workspace_path: '/tmp/my-project'
		};

		expect(status.initialized).toBe(false);
		expect(status.has_project_json).toBe(false);
		expect(status.has_checklist).toBe(false);
		expect(status.has_standards).toBe(false);
		expect(status.sop_count).toBe(0);
		expect(status.workspace_path).toBe('/tmp/my-project');
	});

	it('accepts initialized=true with counts', () => {
		const status: InitStatus = {
			initialized: true,
			has_project_json: true,
			has_checklist: true,
			has_standards: true,
			sop_count: 3,
			workspace_path: '/home/user/repo'
		};

		expect(status.initialized).toBe(true);
		expect(status.sop_count).toBe(3);
	});

	it('uses snake_case field names matching Go json tags', () => {
		const status: InitStatus = {
			initialized: true,
			has_project_json: true,
			has_checklist: false,
			has_standards: false,
			sop_count: 0,
			workspace_path: '/path'
		};

		const requiredSnakeCaseFields = [
			'initialized',
			'has_project_json',
			'has_checklist',
			'has_standards',
			'sop_count',
			'workspace_path'
		];

		for (const field of requiredSnakeCaseFields) {
			expect(field in status).toBe(true);
		}
	});
});

// ---------------------------------------------------------------------------
// DetectedLanguage
// ---------------------------------------------------------------------------

describe('DetectedLanguage', () => {
	it('has all required fields with nullable version', () => {
		const lang: DetectedLanguage = {
			name: 'Go',
			version: '1.23',
			marker: 'go.mod',
			confidence: 'high'
		};

		expect(lang.name).toBe('Go');
		expect(lang.version).toBe('1.23');
		expect(lang.marker).toBe('go.mod');
		expect(lang.confidence).toBe('high');
	});

	it('accepts null version when version is unknown', () => {
		const lang: DetectedLanguage = {
			name: 'Python',
			version: null,
			marker: 'requirements.txt',
			confidence: 'medium'
		};

		expect(lang.version).toBeNull();
		expect(lang.confidence).toBe('medium');
	});

	it('accepts optional primary flag', () => {
		const primary: DetectedLanguage = {
			name: 'Go',
			version: '1.23',
			marker: 'go.mod',
			confidence: 'high',
			primary: true
		};

		const secondary: DetectedLanguage = {
			name: 'TypeScript',
			version: null,
			marker: 'tsconfig.json',
			confidence: 'high'
		};

		expect(primary.primary).toBe(true);
		expect(secondary.primary).toBeUndefined();
	});

	it('confidence is restricted to high or medium', () => {
		const highConf: DetectedLanguage['confidence'] = 'high';
		const medConf: DetectedLanguage['confidence'] = 'medium';

		expect(highConf).toBe('high');
		expect(medConf).toBe('medium');
	});
});

// ---------------------------------------------------------------------------
// DetectedFramework
// ---------------------------------------------------------------------------

describe('DetectedFramework', () => {
	it('has all required fields', () => {
		const fw: DetectedFramework = {
			name: 'SvelteKit',
			language: 'TypeScript',
			marker: 'svelte.config.js',
			confidence: 'high'
		};

		expect(fw.name).toBe('SvelteKit');
		expect(fw.language).toBe('TypeScript');
		expect(fw.marker).toBe('svelte.config.js');
		expect(fw.confidence).toBe('high');
	});
});

// ---------------------------------------------------------------------------
// DetectedTool
// ---------------------------------------------------------------------------

describe('DetectedTool', () => {
	it('has all required fields', () => {
		const tool: DetectedTool = {
			name: 'eslint',
			category: 'linter',
			marker: '.eslintrc.js'
		};

		expect(tool.name).toBe('eslint');
		expect(tool.category).toBe('linter');
		expect(tool.language).toBeUndefined();
	});

	it('accepts optional language field', () => {
		const tool: DetectedTool = {
			name: 'golangci-lint',
			category: 'linter',
			language: 'Go',
			marker: '.golangci.yml'
		};

		expect(tool.language).toBe('Go');
	});

	it('category covers all valid tool types', () => {
		const categories: DetectedTool['category'][] = [
			'linter',
			'formatter',
			'task_runner',
			'ci',
			'container',
			'test_framework',
			'type_checker'
		];

		// All categories are assignable - verified by TypeScript
		expect(categories).toHaveLength(7);
		expect(categories).toContain('linter');
		expect(categories).toContain('type_checker');
	});
});

// ---------------------------------------------------------------------------
// DetectedDoc
// ---------------------------------------------------------------------------

describe('DetectedDoc', () => {
	it('has all required fields', () => {
		const doc: DetectedDoc = {
			path: 'CLAUDE.md',
			type: 'claude_instructions',
			size_bytes: 4096
		};

		expect(doc.path).toBe('CLAUDE.md');
		expect(doc.type).toBe('claude_instructions');
		expect(doc.size_bytes).toBe(4096);
	});

	it('type covers all expected documentation categories', () => {
		const types: DetectedDoc['type'][] = [
			'project_docs',
			'contributing',
			'claude_instructions',
			'existing_sop',
			'architecture_docs'
		];

		expect(types).toHaveLength(5);
		expect(types).toContain('existing_sop');
	});
});

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

describe('Check', () => {
	it('has all required fields', () => {
		const check: Check = {
			name: 'Go Tests',
			command: 'go test ./...',
			trigger: ['pre-commit', 'ci'],
			category: 'test',
			required: true,
			timeout: '120s',
			description: 'Run all Go unit tests'
		};

		expect(check.name).toBe('Go Tests');
		expect(check.command).toBe('go test ./...');
		expect(check.trigger).toHaveLength(2);
		expect(check.category).toBe('test');
		expect(check.required).toBe(true);
		expect(check.timeout).toBe('120s');
		expect(check.description).toBe('Run all Go unit tests');
	});

	it('accepts optional working_dir', () => {
		const check: Check = {
			name: 'UI Tests',
			command: 'npm run test',
			trigger: [],
			category: 'test',
			required: false,
			timeout: '60s',
			description: 'Run frontend tests',
			working_dir: './ui'
		};

		expect(check.working_dir).toBe('./ui');
	});

	it('category covers all valid check types', () => {
		const categories: Check['category'][] = [
			'compile',
			'lint',
			'typecheck',
			'test',
			'format'
		];

		expect(categories).toHaveLength(5);
		expect(categories).toContain('compile');
		expect(categories).toContain('format');
	});
});

// ---------------------------------------------------------------------------
// DetectionResult
// ---------------------------------------------------------------------------

describe('DetectionResult', () => {
	it('has all required array fields', () => {
		const result: DetectionResult = {
			languages: [],
			frameworks: [],
			tooling: [],
			existing_docs: [],
			proposed_checklist: []
		};

		expect(Array.isArray(result.languages)).toBe(true);
		expect(Array.isArray(result.frameworks)).toBe(true);
		expect(Array.isArray(result.tooling)).toBe(true);
		expect(Array.isArray(result.existing_docs)).toBe(true);
		expect(Array.isArray(result.proposed_checklist)).toBe(true);
	});

	it('a realistic detection result is correctly typed', () => {
		const result: DetectionResult = {
			languages: [
				{ name: 'Go', version: '1.23', marker: 'go.mod', confidence: 'high', primary: true },
				{ name: 'TypeScript', version: null, marker: 'tsconfig.json', confidence: 'high' }
			],
			frameworks: [
				{ name: 'SvelteKit', language: 'TypeScript', marker: 'svelte.config.js', confidence: 'high' }
			],
			tooling: [
				{ name: 'Task', category: 'task_runner', marker: 'Taskfile.yml' },
				{ name: 'golangci-lint', category: 'linter', language: 'Go', marker: '.golangci.yml' }
			],
			existing_docs: [
				{ path: 'CLAUDE.md', type: 'claude_instructions', size_bytes: 8192 }
			],
			proposed_checklist: [
				{
					name: 'Go Build',
					command: 'go build ./...',
					trigger: [],
					category: 'compile',
					required: true,
					timeout: '60s',
					description: 'Verify the project compiles'
				}
			]
		};

		expect(result.languages[0].primary).toBe(true);
		expect(result.languages[1].primary).toBeUndefined();
		expect(result.tooling[1].language).toBe('Go');
		expect(result.proposed_checklist[0].category).toBe('compile');
	});

	it('uses snake_case field names matching Go json tags', () => {
		const result: DetectionResult = {
			languages: [],
			frameworks: [],
			tooling: [],
			existing_docs: [],
			proposed_checklist: []
		};

		expect('existing_docs' in result).toBe(true);
		expect('proposed_checklist' in result).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// Rule
// ---------------------------------------------------------------------------

describe('Rule', () => {
	it('has all required fields', () => {
		const rule: Rule = {
			id: 'rule-001',
			text: 'Always return errors rather than panicking',
			severity: 'error',
			category: 'error-handling',
			origin: 'CLAUDE.md'
		};

		expect(rule.id).toBe('rule-001');
		expect(rule.text).toBeDefined();
		expect(rule.severity).toBe('error');
		expect(rule.category).toBe('error-handling');
		expect(rule.origin).toBe('CLAUDE.md');
	});

	it('accepts optional applies_to field', () => {
		const rule: Rule = {
			id: 'rule-002',
			text: 'Use context.Context as first parameter for I/O operations',
			severity: 'warning',
			category: 'context-management',
			applies_to: ['*.go'],
			origin: 'CLAUDE.md'
		};

		expect(rule.applies_to).toEqual(['*.go']);
	});

	it('severity covers all three valid levels', () => {
		const severities: Rule['severity'][] = ['error', 'warning', 'info'];

		expect(severities).toHaveLength(3);
		expect(severities).toContain('error');
		expect(severities).toContain('info');
	});
});

// ---------------------------------------------------------------------------
// GenerateStandardsResponse
// ---------------------------------------------------------------------------

describe('GenerateStandardsResponse', () => {
	it('has rules array and token_estimate', () => {
		const response: GenerateStandardsResponse = {
			rules: [],
			token_estimate: 1500
		};

		expect(Array.isArray(response.rules)).toBe(true);
		expect(response.token_estimate).toBe(1500);
	});

	it('uses snake_case for token_estimate matching Go json tag', () => {
		const response: GenerateStandardsResponse = {
			rules: [],
			token_estimate: 0
		};

		expect('token_estimate' in response).toBe(true);
		expect('tokenEstimate' in response).toBe(false);
	});
});

// ---------------------------------------------------------------------------
// ProjectInitInput
// ---------------------------------------------------------------------------

describe('ProjectInitInput', () => {
	it('has required fields and optional fields', () => {
		const minimal: ProjectInitInput = {
			name: 'semspec',
			languages: ['Go', 'TypeScript'],
			frameworks: ['SvelteKit']
		};

		expect(minimal.name).toBe('semspec');
		expect(minimal.description).toBeUndefined();
		expect(minimal.repository).toBeUndefined();
	});

	it('accepts full input with all optional fields', () => {
		const full: ProjectInitInput = {
			name: 'semspec',
			description: 'Semantic development agent',
			languages: ['Go', 'TypeScript'],
			frameworks: ['SvelteKit'],
			repository: 'https://github.com/c360/semspec'
		};

		expect(full.description).toBe('Semantic development agent');
		expect(full.repository).toContain('github.com');
	});
});

// ---------------------------------------------------------------------------
// InitRequest
// ---------------------------------------------------------------------------

describe('InitRequest', () => {
	it('has all required top-level fields', () => {
		const req: InitRequest = {
			project: {
				name: 'test-project',
				languages: ['Go'],
				frameworks: []
			},
			checklist: [],
			standards: {
				version: '1.0.0',
				rules: []
			}
		};

		expect(req.project.name).toBe('test-project');
		expect(Array.isArray(req.checklist)).toBe(true);
		expect(req.standards.version).toBe('1.0.0');
		expect(Array.isArray(req.standards.rules)).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// InitResponse
// ---------------------------------------------------------------------------

describe('InitResponse', () => {
	it('has success flag and files_written array', () => {
		const response: InitResponse = {
			success: true,
			files_written: ['.semspec/project.json', '.semspec/checklist.yaml']
		};

		expect(response.success).toBe(true);
		expect(response.files_written).toHaveLength(2);
	});

	it('uses snake_case files_written matching Go json tag', () => {
		const response: InitResponse = {
			success: false,
			files_written: []
		};

		expect('files_written' in response).toBe(true);
		expect('filesWritten' in response).toBe(false);
	});
});

// ---------------------------------------------------------------------------
// Field name consistency (snake_case from Go json tags)
// ---------------------------------------------------------------------------

describe('field name consistency', () => {
	it('all multi-word fields use snake_case matching Go json tags', () => {
		// This test documents the snake_case contract between Go and TypeScript.
		// If Go renames a json tag, these references will cause TypeScript errors.
		const status: InitStatus = {
			initialized: false,
			has_project_json: false,
			has_checklist: false,
			has_standards: false,
			sop_count: 0,
			workspace_path: '/'
		};

		const multiWordFields: (keyof InitStatus)[] = [
			'has_project_json',
			'has_checklist',
			'has_standards',
			'sop_count',
			'workspace_path'
		];

		for (const field of multiWordFields) {
			expect(field in status).toBe(true);
		}
	});

	it('Check fields use snake_case', () => {
		const check: Check = {
			name: 'test',
			command: 'go test',
			trigger: [],
			category: 'test',
			required: true,
			timeout: '60s',
			description: 'tests',
			working_dir: './'
		};

		expect('working_dir' in check).toBe(true);
		expect('workingDir' in check).toBe(false);
	});

	it('DetectedDoc uses snake_case for size_bytes', () => {
		const doc: DetectedDoc = {
			path: 'README.md',
			type: 'project_docs',
			size_bytes: 1024
		};

		expect('size_bytes' in doc).toBe(true);
		expect('sizeBytes' in doc).toBe(false);
	});
});
