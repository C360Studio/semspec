import type { Loop, ActivityEvent, MessageResponse } from '$lib/types';
import { mockPlans, mockTasks, mockPhases } from './mock-plans';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';
import type { Phase } from '$lib/types/phase';
import type { SynthesisResult } from '$lib/types/review';
import type { ContextBuildResponse } from '$lib/types/context';

// Simulated delay for realistic UX
function delay(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

// Sample data - matches backend LoopInfo struct
const sampleLoops: Loop[] = [
	{
		loop_id: 'loop_abc123',
		task_id: 'task_001',
		user_id: 'user_default',
		channel_type: 'http',
		channel_id: 'chat',
		state: 'executing',
		iterations: 3,
		max_iterations: 10,
		created_at: new Date().toISOString()
	},
	{
		loop_id: 'loop_def456',
		task_id: 'task_002',
		user_id: 'user_default',
		channel_type: 'http',
		channel_id: 'chat',
		state: 'paused',
		iterations: 5,
		max_iterations: 10,
		created_at: new Date(Date.now() - 300000).toISOString()
	}
];

// Mock response generators
const mockResponses: string[] = [
	"I understand. Let me analyze that for you.",
	"I'll help you with that. Here's what I found...",
	"That's a great question. Based on my analysis...",
	"I've reviewed the codebase and have some suggestions.",
	"Let me work on that. I'll need to check a few things first."
];

// Mock synthesis result for reviews
const mockSynthesisResult: SynthesisResult = {
	request_id: 'req-mock-001',
	workflow_id: 'add-user-authentication',
	verdict: 'needs_changes',
	passed: false,
	findings: [
		{
			role: 'security_reviewer',
			category: 'injection',
			severity: 'high',
			file: 'src/api/auth.go',
			line: 89,
			issue: 'SQL query uses string concatenation instead of parameterized queries',
			suggestion: 'Use parameterized queries: db.Query("SELECT * FROM users WHERE id = ?", userId)',
			cwe: 'CWE-89'
		},
		{
			role: 'style_reviewer',
			category: 'naming',
			severity: 'medium',
			file: 'src/api/auth.go',
			line: 45,
			issue: 'Function name GetUserData does not follow Go conventions',
			suggestion: 'Rename to getUserData for unexported function or keep as GetUserData if exported'
		},
		{
			role: 'sop_reviewer',
			category: 'error-handling',
			severity: 'medium',
			file: 'src/api/auth.go',
			line: 102,
			issue: 'Error returned without wrapping context',
			suggestion: 'Wrap error with context: fmt.Errorf("failed to validate token: %w", err)',
			sop_id: 'sop:error-handling',
			status: 'violated'
		}
	],
	reviewers: [
		{
			role: 'spec_reviewer',
			passed: true,
			summary: 'Implementation matches specification. All required endpoints implemented.',
			finding_count: 0,
			verdict: 'compliant'
		},
		{
			role: 'sop_reviewer',
			passed: false,
			summary: 'One error handling violation found.',
			finding_count: 1
		},
		{
			role: 'style_reviewer',
			passed: false,
			summary: 'Minor naming convention issue found.',
			finding_count: 1
		},
		{
			role: 'security_reviewer',
			passed: false,
			summary: 'SQL injection vulnerability detected.',
			finding_count: 1
		}
	],
	summary:
		'Review complete: 1/4 reviewers passed. Found 3 issues requiring attention before approval.',
	stats: {
		total_findings: 3,
		by_severity: {
			critical: 0,
			high: 1,
			medium: 2,
			low: 0
		},
		by_reviewer: {
			security_reviewer: 1,
			style_reviewer: 1,
			sop_reviewer: 1
		},
		reviewers_total: 4,
		reviewers_passed: 1
	}
};

// Mock context build response
const mockContextResponse: ContextBuildResponse = {
	request_id: 'ctx-mock-001',
	task_type: 'review',
	token_count: 24500,
	provenance: [
		{ source: 'sop:error-handling', type: 'sop', tokens: 1247, priority: 1 },
		{ source: 'sop:logging', type: 'sop', tokens: 892, priority: 1 },
		{ source: 'git:HEAD~1..HEAD', type: 'git_diff', tokens: 2456, priority: 2 },
		{ source: 'file:src/api/auth_test.go', type: 'test', tokens: 456, truncated: true, priority: 3 },
		{ source: 'entity:naming-conventions', type: 'convention', tokens: 312, priority: 4 }
	],
	sop_ids: ['sop:error-handling', 'sop:logging'],
	tokens_used: 24500,
	tokens_budget: 32000,
	truncated: true
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type MockHandler = (body?: any, slug?: string) => Promise<any>;

// Mutable copy of plans for mock mutations
let mutablePlans = structuredClone(mockPlans);
let mutablePhases: Record<string, Phase[]> = structuredClone(mockPhases);
let mutableTasks: Record<string, Task[]> = structuredClone(mockTasks);

const mockHandlers: Record<string, MockHandler> = {
	'GET /agentic-dispatch/loops': async () => {
		await delay(200);
		return sampleLoops;
	},

	// Plan mutations
	'POST /workflow-api/plans/promote': async (_body, slug?: string) => {
		await delay(300);
		const plan = mutablePlans.find((p) => p.slug === slug);
		if (plan) {
			plan.approved = true;
			plan.approved_at = new Date().toISOString();
			plan.stage = 'approved';
		}
		return plan;
	},

	'POST /workflow-api/plans/generate-tasks': async (_body, slug?: string) => {
		await delay(500);
		const plan = mutablePlans.find((p) => p.slug === slug);
		if (plan) {
			plan.stage = 'tasks';
			// Return empty tasks for now - they'd be generated
			return [];
		}
		return [];
	},

	'POST /workflow-api/plans/execute': async (_body, slug?: string) => {
		await delay(300);
		const plan = mutablePlans.find((p) => p.slug === slug);
		if (plan) {
			plan.stage = 'executing';
		}
		return plan;
	},

	'POST /agentic-dispatch/message': async () => {
		await delay(800 + Math.random() * 400);
		const response: MessageResponse = {
			response_id: `resp_${Date.now()}`,
			type: 'assistant_response',
			content: mockResponses[Math.floor(Math.random() * mockResponses.length)],
			timestamp: new Date().toISOString(),
			in_reply_to: Math.random() > 0.7 ? 'loop_abc123' : undefined
		};
		return response;
	},

	'GET /agentic-dispatch/health': async () => {
		await delay(100);
		return {
			healthy: true,
			components: [
				{ name: 'router', status: 'running', uptime: 3600 },
				{ name: 'loop', status: 'running', uptime: 3600 },
				{ name: 'model', status: 'running', uptime: 3600 }
			]
		};
	},

	// Project API - return initialized project status
	'GET /project-api/status': async () => {
		await delay(100);
		return {
			initialized: true,
			project_name: 'workspace ui test',
			project_description: 'A test project for UI development',
			has_project_json: true,
			has_checklist: true,
			has_standards: true,
			sop_count: 3,
			workspace_path: '/workspace'
		};
	},

	// Project API - wizard options
	'GET /project-api/wizard': async () => {
		await delay(100);
		return {
			languages: [
				{ name: 'Go', marker: 'go.mod', has_ast: true },
				{ name: 'TypeScript', marker: 'tsconfig.json', has_ast: true }
			],
			frameworks: [
				{ name: 'SvelteKit', language: 'TypeScript' },
				{ name: 'Echo', language: 'Go' }
			]
		};
	},

	// Project API - detection
	'POST /project-api/detect': async () => {
		await delay(200);
		return {
			languages: [],
			frameworks: [],
			tooling: [],
			existing_docs: [],
			proposed_checklist: []
		};
	},

	// Workflow API plans
	'GET /workflow-api/plans': async (): Promise<PlanWithStatus[]> => {
		await delay(200);
		return mutablePlans;
	},

	// Workflow API questions (empty by default)
	'GET /workflow-api/questions': async () => {
		await delay(100);
		return [];
	},

	'GET /workflow/plans': async (): Promise<PlanWithStatus[]> => {
		await delay(200);
		return mockPlans;
	},

	'GET /workflow/plans/add-user-authentication': async (): Promise<PlanWithStatus | undefined> => {
		await delay(100);
		return mockPlans.find((p) => p.slug === 'add-user-authentication');
	},

	'GET /workflow/plans/add-user-authentication/tasks': async (): Promise<Task[]> => {
		await delay(100);
		return mockTasks['add-user-authentication'] || [];
	},

	'GET /workflow-api/plans/add-user-authentication/reviews': async (): Promise<SynthesisResult> => {
		await delay(150);
		return mockSynthesisResult;
	},

	'GET /context-builder/responses/ctx-mock-001': async (): Promise<ContextBuildResponse> => {
		await delay(150);
		return mockContextResponse;
	}
};

export async function mockRequest<T>(
	path: string,
	options: { method?: string; body?: unknown } = {}
): Promise<T> {
	const method = options.method || 'GET';
	const cleanPath = path.split('?')[0];
	const key = `${method} ${cleanPath}`;

	// Try exact match first
	let handler = mockHandlers[key];
	if (handler) {
		return handler(options.body) as Promise<T>;
	}

	// Try pattern matching for dynamic routes
	// POST /workflow-api/plans/{slug}/promote
	const promoteMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/promote$/);
	if (method === 'POST' && promoteMatch) {
		handler = mockHandlers['POST /workflow-api/plans/promote'];
		if (handler) return handler(options.body, promoteMatch[1]) as Promise<T>;
	}

	// POST /workflow-api/plans/{slug}/generate-tasks
	const generateMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/generate-tasks$/);
	if (method === 'POST' && generateMatch) {
		handler = mockHandlers['POST /workflow-api/plans/generate-tasks'];
		if (handler) return handler(options.body, generateMatch[1]) as Promise<T>;
	}

	// POST /workflow-api/plans/{slug}/execute
	const executeMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/execute$/);
	if (method === 'POST' && executeMatch) {
		handler = mockHandlers['POST /workflow-api/plans/execute'];
		if (handler) return handler(options.body, executeMatch[1]) as Promise<T>;
	}

	// GET /workflow-api/plans/{slug}
	const planMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)$/);
	if (method === 'GET' && planMatch) {
		await delay(100);
		return mutablePlans.find((p) => p.slug === planMatch[1]) as T;
	}

	// GET /workflow-api/plans/{slug}/tasks
	const tasksMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/tasks$/);
	if (method === 'GET' && tasksMatch) {
		await delay(100);
		return (mutableTasks[tasksMatch[1]] || []) as T;
	}

	// GET /workflow-api/plans/{slug}/phases
	const phasesMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/phases$/);
	if (method === 'GET' && phasesMatch) {
		await delay(100);
		return (mutablePhases[phasesMatch[1]] || []) as T;
	}

	// POST /workflow-api/plans/{slug}/phases (create phase)
	if (method === 'POST' && phasesMatch) {
		await delay(200);
		const slug = phasesMatch[1];
		const body = options.body as { name: string; description?: string; depends_on?: string[]; requires_approval?: boolean };
		const phases = mutablePhases[slug] || [];
		const newPhase: Phase = {
			id: `phase.${slug}.${phases.length + 1}`,
			plan_id: `plan.${slug}`,
			sequence: phases.length + 1,
			name: body.name,
			description: body.description,
			depends_on: body.depends_on,
			status: 'pending',
			requires_approval: body.requires_approval,
			created_at: new Date().toISOString()
		};
		mutablePhases[slug] = [...phases, newPhase];
		return newPhase as T;
	}

	// GET /workflow-api/plans/{slug}/phases/{phaseId}
	const phaseGetMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/phases\/([^/]+)$/);
	if (method === 'GET' && phaseGetMatch) {
		await delay(100);
		const [, slug, phaseId] = phaseGetMatch;
		const phases = mutablePhases[slug] || [];
		return phases.find((p) => p.id === phaseId) as T;
	}

	// PATCH /workflow-api/plans/{slug}/phases/{phaseId}
	if (method === 'PATCH' && phaseGetMatch) {
		await delay(150);
		const [, slug, phaseId] = phaseGetMatch;
		const phases = mutablePhases[slug] || [];
		const idx = phases.findIndex((p) => p.id === phaseId);
		if (idx !== -1) {
			const body = options.body as Partial<Phase>;
			phases[idx] = { ...phases[idx], ...body };
			mutablePhases[slug] = [...phases];
			return phases[idx] as T;
		}
		return {} as T;
	}

	// DELETE /workflow-api/plans/{slug}/phases/{phaseId}
	if (method === 'DELETE' && phaseGetMatch) {
		await delay(150);
		const [, slug, phaseId] = phaseGetMatch;
		const phases = mutablePhases[slug] || [];
		mutablePhases[slug] = phases.filter((p) => p.id !== phaseId);
		return undefined as T;
	}

	// POST /workflow-api/plans/{slug}/phases/{phaseId}/approve
	const phaseApproveMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/phases\/([^/]+)\/approve$/);
	if (method === 'POST' && phaseApproveMatch) {
		await delay(200);
		const [, slug, phaseId] = phaseApproveMatch;
		const phases = mutablePhases[slug] || [];
		const idx = phases.findIndex((p) => p.id === phaseId);
		if (idx !== -1) {
			phases[idx] = {
				...phases[idx],
				approved: true,
				approved_at: new Date().toISOString(),
				status: 'ready'
			};
			mutablePhases[slug] = [...phases];
			return phases[idx] as T;
		}
		return {} as T;
	}

	// POST /workflow-api/plans/{slug}/phases/generate
	const phasesGenerateMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/phases\/generate$/);
	if (method === 'POST' && phasesGenerateMatch) {
		await delay(500);
		const slug = phasesGenerateMatch[1];
		const plan = mutablePlans.find((p) => p.slug === slug);
		if (plan) {
			// Generate sample phases for the plan
			const generatedPhases: Phase[] = [
				{
					id: `phase.${slug}.1`,
					plan_id: plan.id,
					sequence: 1,
					name: 'Phase 1: Setup',
					description: 'Initial setup and foundation work',
					status: 'pending',
					requires_approval: true,
					created_at: new Date().toISOString()
				},
				{
					id: `phase.${slug}.2`,
					plan_id: plan.id,
					sequence: 2,
					name: 'Phase 2: Implementation',
					description: 'Core implementation work',
					depends_on: [`phase.${slug}.1`],
					status: 'pending',
					requires_approval: true,
					created_at: new Date().toISOString()
				},
				{
					id: `phase.${slug}.3`,
					plan_id: plan.id,
					sequence: 3,
					name: 'Phase 3: Testing',
					description: 'Testing and validation',
					depends_on: [`phase.${slug}.2`],
					status: 'pending',
					requires_approval: true,
					created_at: new Date().toISOString()
				}
			];
			mutablePhases[slug] = generatedPhases;
			plan.phases = generatedPhases;
			plan.phase_stats = {
				total: 3,
				pending: 3,
				ready: 0,
				active: 0,
				complete: 0,
				failed: 0,
				blocked: 0
			};
			return generatedPhases as T;
		}
		return [] as T;
	}

	// POST /workflow-api/plans/{slug}/phases/approve (approve all)
	const phasesApproveAllMatch = cleanPath.match(/^\/workflow-api\/plans\/([^/]+)\/phases\/approve$/);
	if (method === 'POST' && phasesApproveAllMatch) {
		await delay(300);
		const slug = phasesApproveAllMatch[1];
		const phases = mutablePhases[slug] || [];
		const now = new Date().toISOString();
		mutablePhases[slug] = phases.map((p) => ({
			...p,
			approved: true,
			approved_at: now,
			status: p.depends_on?.length ? 'blocked' : 'ready'
		}));
		const plan = mutablePlans.find((p) => p.slug === slug);
		if (plan) {
			plan.phases = mutablePhases[slug];
		}
		return plan as T;
	}

	// Default fallback
	await delay(200);
	console.warn(`[Mock] No handler for ${key}`);
	return {} as T;
}

// Mock activity event generator for SSE simulation
let activityInterval: ReturnType<typeof setInterval> | null = null;
let activityListeners: ((event: ActivityEvent) => void)[] = [];

export function startMockActivityStream(): void {
	if (activityInterval) return;

	const eventTypes: ActivityEvent['type'][] = ['loop_created', 'loop_updated', 'loop_deleted'];

	activityInterval = setInterval(() => {
		const event: ActivityEvent = {
			type: eventTypes[Math.floor(Math.random() * eventTypes.length)],
			loop_id: 'loop_abc123',
			timestamp: new Date().toISOString(),
			data: JSON.stringify({
				state: 'executing',
				iterations: Math.floor(Math.random() * 10)
			})
		};
		activityListeners.forEach((listener) => listener(event));
	}, 3000 + Math.random() * 2000);
}

export function stopMockActivityStream(): void {
	if (activityInterval) {
		clearInterval(activityInterval);
		activityInterval = null;
	}
}

export function addActivityListener(listener: (event: ActivityEvent) => void): () => void {
	activityListeners.push(listener);
	return () => {
		activityListeners = activityListeners.filter((l) => l !== listener);
	};
}
