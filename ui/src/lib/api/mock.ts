import type { Loop, ActivityEvent, MessageResponse } from '$lib/types';
import { mockPlans, mockTasks } from './mock-plans';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';
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
type MockHandler = (body?: any) => Promise<any>;

const mockHandlers: Record<string, MockHandler> = {
	'GET /agentic-dispatch/loops': async () => {
		await delay(200);
		return sampleLoops;
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
	const key = `${method} ${path.split('?')[0]}`;

	const handler = mockHandlers[key];
	if (handler) {
		return handler(options.body) as Promise<T>;
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
