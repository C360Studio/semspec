import type { Message, Loop, ActivityEvent } from '$lib/types';

// Simulated delay for realistic UX
function delay(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

// Sample data
const sampleLoops: Loop[] = [
	{
		id: 'loop-abc123',
		state: 'executing',
		role: 'developer',
		model: 'claude-3-5-sonnet',
		iterations: 3,
		maxIterations: 10,
		owner: 'user',
		source: 'chat',
		pendingTools: ['file_read'],
		startedAt: new Date().toISOString(),
		prompt: 'Help me refactor the authentication module'
	},
	{
		id: 'loop-def456',
		state: 'awaiting_approval',
		role: 'reviewer',
		model: 'claude-3-5-sonnet',
		iterations: 5,
		maxIterations: 10,
		owner: 'user',
		source: 'chat',
		pendingTools: [],
		startedAt: new Date(Date.now() - 300000).toISOString(),
		prompt: 'Review the PR changes'
	}
];

const sampleMessages: Message[] = [
	{
		id: 'msg-1',
		type: 'assistant',
		content:
			"Welcome to Semspec! I'm your development assistant. How can I help you today?",
		timestamp: new Date(Date.now() - 60000).toISOString()
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

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type MockHandler = (body?: any) => Promise<any>;

const mockHandlers: Record<string, MockHandler> = {
	'GET /api/router/loops': async () => {
		await delay(200);
		return { loops: sampleLoops, total: sampleLoops.length };
	},

	'POST /api/router/message': async (body: { content: string }) => {
		await delay(800 + Math.random() * 400);
		const response: Message = {
			id: `msg-${Date.now()}`,
			type: 'assistant',
			content: mockResponses[Math.floor(Math.random() * mockResponses.length)],
			timestamp: new Date().toISOString(),
			loopId: Math.random() > 0.7 ? 'loop-abc123' : undefined
		};
		return response;
	},

	'GET /api/health': async () => {
		await delay(100);
		return {
			healthy: true,
			components: [
				{ name: 'router', status: 'running', uptime: 3600 },
				{ name: 'loop', status: 'running', uptime: 3600 },
				{ name: 'model', status: 'running', uptime: 3600 }
			]
		};
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

	const eventTypes: ActivityEvent['type'][] = [
		'tool_call',
		'tool_result',
		'model_request',
		'model_response',
		'status_update'
	];

	activityInterval = setInterval(() => {
		const event: ActivityEvent = {
			type: eventTypes[Math.floor(Math.random() * eventTypes.length)],
			loop_id: 'loop-abc123',
			timestamp: new Date().toISOString(),
			data: {
				tool: 'file_read',
				duration_ms: Math.floor(Math.random() * 500)
			}
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
