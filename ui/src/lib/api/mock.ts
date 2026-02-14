import type { Loop, ActivityEvent, MessageResponse } from '$lib/types';
import { mockPlans, mockTasks } from './mock-plans';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Task } from '$lib/types/task';

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
