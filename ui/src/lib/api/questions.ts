import type { Question, QuestionStatus } from '$lib/types';
import { request } from './client';

const BASE_URL = import.meta.env.VITE_API_URL || '';

/**
 * Question list filter parameters.
 */
export interface QuestionFilters {
	status?: QuestionStatus | 'all';
	topic?: string;
	limit?: number;
}

/**
 * Answer submission request.
 */
export interface AnswerRequest {
	answer: string;
	answered_by?: string;
	answerer_type?: 'human' | 'agent' | 'team';
	confidence?: 'high' | 'medium' | 'low';
	sources?: string;
}

/**
 * SSE event types from the questions stream.
 */
export type QuestionEventType = 'question_created' | 'question_answered' | 'question_timeout' | 'heartbeat';

/**
 * SSE event from the questions stream.
 */
export interface QuestionEvent {
	type: QuestionEventType;
	data: Question | Record<string, never>; // heartbeat has empty data
}

/**
 * Questions REST + SSE API client.
 *
 * Endpoints:
 * - GET /workflow-api/questions - List questions (filterable)
 * - GET /workflow-api/questions/{id} - Get single question
 * - POST /workflow-api/questions/{id}/answer - Submit answer
 * - GET /workflow-api/questions/stream - SSE real-time events
 */
export const questionsApi = {
	/**
	 * List questions with optional filters.
	 */
	async list(filters?: QuestionFilters): Promise<Question[]> {
		const params = new URLSearchParams();
		if (filters?.status && filters.status !== 'all') {
			params.set('status', filters.status);
		}
		if (filters?.topic) {
			params.set('topic', filters.topic);
		}
		if (filters?.limit) {
			params.set('limit', String(filters.limit));
		}
		const query = params.toString();
		const path = `/workflow-api/questions${query ? `?${query}` : ''}`;
		const response = await request<{ questions: Question[]; total: number }>(path);
		return response.questions;
	},

	/**
	 * Get a single question by ID.
	 */
	async get(id: string): Promise<Question> {
		return request<Question>(`/workflow-api/questions/${id}`);
	},

	/**
	 * Submit an answer to a question.
	 */
	async answer(id: string, req: AnswerRequest): Promise<void> {
		await request<void>(`/workflow-api/questions/${id}/answer`, {
			method: 'POST',
			body: req
		});
	},

	/**
	 * Subscribe to the questions SSE stream.
	 * Returns an unsubscribe function.
	 *
	 * @param onEvent - Callback for each event
	 * @param onError - Optional error callback
	 * @returns Unsubscribe function to close the connection
	 */
	subscribeToStream(
		onEvent: (event: QuestionEvent) => void,
		onError?: (error: Error) => void
	): () => void {
		const url = `${BASE_URL}/workflow-api/questions/stream`;
		const eventSource = new EventSource(url);

		// Handle specific event types
		const handleEvent = (type: QuestionEventType) => (e: MessageEvent) => {
			try {
				const data = e.data ? JSON.parse(e.data) : {};
				onEvent({ type, data });
			} catch (err) {
				console.error(`Failed to parse ${type} event:`, err);
			}
		};

		eventSource.addEventListener('question_created', handleEvent('question_created'));
		eventSource.addEventListener('question_answered', handleEvent('question_answered'));
		eventSource.addEventListener('question_timeout', handleEvent('question_timeout'));
		eventSource.addEventListener('heartbeat', handleEvent('heartbeat'));

		// Handle connection errors
		eventSource.onerror = (e) => {
			console.error('Questions SSE error:', e);
			onError?.(new Error('Questions stream connection error'));
		};

		// Return unsubscribe function
		return () => {
			eventSource.close();
		};
	}
};
