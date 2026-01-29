import { mockRequest } from './mock';
import type { Message, Loop } from '$lib/types';

const BASE_URL = import.meta.env.VITE_API_URL || '';
const USE_MOCKS = import.meta.env.VITE_USE_MOCKS === 'true';

interface RequestOptions {
	method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
	body?: unknown;
	headers?: Record<string, string>;
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
	if (USE_MOCKS) {
		return mockRequest<T>(path, options);
	}

	const { method = 'GET', body, headers = {} } = options;

	const response = await fetch(`${BASE_URL}${path}`, {
		method,
		headers: {
			'Content-Type': 'application/json',
			...headers
		},
		body: body ? JSON.stringify(body) : undefined
	});

	if (!response.ok) {
		const error = await response.json().catch(() => ({ message: response.statusText }));
		throw new Error(error.message || `Request failed: ${response.status}`);
	}

	return response.json();
}

function toQueryString(params?: Record<string, unknown>): string {
	if (!params) return '';
	const entries = Object.entries(params).filter(([, v]) => v !== undefined);
	if (entries.length === 0) return '';
	return '?' + new URLSearchParams(entries.map(([k, v]) => [k, String(v)])).toString();
}

export const api = {
	router: {
		getLoops: (params?: { owner?: string; state?: string }) =>
			request<{ loops: Loop[]; total: number }>(`/api/router/loops${toQueryString(params)}`),

		getLoop: (id: string) => request<Loop>(`/api/router/loops/${id}`),

		sendSignal: (loopId: string, type: string, payload?: unknown) =>
			request(`/api/router/loops/${loopId}/signal`, {
				method: 'POST',
				body: { type, payload }
			}),

		sendMessage: (content: string) =>
			request<Message>('/api/router/message', {
				method: 'POST',
				body: { content }
			})
	},

	system: {
		getHealth: () => request('/api/health')
	}
};
