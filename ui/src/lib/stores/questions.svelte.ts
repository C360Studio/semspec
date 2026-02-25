import { questionsApi, type QuestionEvent } from '$lib/api/questions';
import type { Question, QuestionStatus } from '$lib/types';

/**
 * Questions store for Knowledge Gap Resolution Protocol.
 *
 * Questions are fetched via REST API and updated in real-time via SSE.
 * Questions are created by agents (via context-builder) and answered by humans.
 */
class QuestionsStore {
	all = $state<Question[]>([]);
	loading = $state(false);
	error = $state<string | null>(null);
	lastRefresh = $state<Date | null>(null);
	connected = $state(false);

	private unsubscribe: (() => void) | null = null;

	get pending(): Question[] {
		return (this.all ?? []).filter((q) => q.status === 'pending');
	}

	get answered(): Question[] {
		return (this.all ?? []).filter((q) => q.status === 'answered');
	}

	get timedOut(): Question[] {
		return (this.all ?? []).filter((q) => q.status === 'timeout');
	}

	get blocking(): Question[] {
		return this.pending.filter((q) => q.urgency === 'blocking');
	}

	/**
	 * Connect to the questions SSE stream for real-time updates.
	 */
	connect(): void {
		if (this.unsubscribe) {
			return; // Already connected
		}

		try {
			this.unsubscribe = questionsApi.subscribeToStream(
				(event: QuestionEvent) => {
					this.handleEvent(event);
				},
				(error: Error) => {
					console.error('Questions stream error:', error);
					this.connected = false;
					// Attempt reconnect after delay
					setTimeout(() => this.reconnect(), 10000);
				}
			);
			this.connected = true;
		} catch (err) {
			console.error('Failed to connect to questions stream:', err);
			this.connected = false;
		}

		// Initial fetch to populate state (non-blocking)
		this.fetch().catch((err) => {
			console.error('Failed to fetch questions:', err);
		});
	}

	/**
	 * Disconnect from the SSE stream.
	 */
	disconnect(): void {
		if (this.unsubscribe) {
			this.unsubscribe();
			this.unsubscribe = null;
		}
		this.connected = false;
	}

	/**
	 * Reconnect to the SSE stream.
	 */
	private reconnect(): void {
		this.disconnect();
		this.connect();
	}

	/**
	 * Handle SSE events.
	 */
	private handleEvent(event: QuestionEvent): void {
		switch (event.type) {
			case 'question_created':
				this.addQuestion(event.data as Question);
				break;
			case 'question_answered':
				this.updateQuestion((event.data as Question).id, event.data as Partial<Question>);
				break;
			case 'question_timeout':
				this.updateQuestion((event.data as Question).id, { status: 'timeout' });
				break;
			case 'heartbeat':
				// Heartbeat - connection is alive
				break;
		}
	}

	/**
	 * Add a new question to the store.
	 */
	private addQuestion(question: Question): void {
		// Check for duplicate
		if (this.all.find((q) => q.id === question.id)) {
			return;
		}
		this.all = [...this.all, question];
	}

	/**
	 * Update a question in the store.
	 */
	private updateQuestion(id: string, updates: Partial<Question>): void {
		this.all = this.all.map((q) => (q.id === id ? { ...q, ...updates } : q));
	}

	/**
	 * Fetch questions from the API.
	 */
	async fetch(status?: QuestionStatus | 'all'): Promise<void> {
		this.loading = true;
		this.error = null;

		try {
			this.all = await questionsApi.list({ status });
			this.lastRefresh = new Date();
		} catch (err) {
			this.error = err instanceof Error ? err.message : 'Failed to fetch questions';
		} finally {
			this.loading = false;
		}
	}

	/**
	 * Answer a question.
	 */
	async answer(questionId: string, response: string): Promise<void> {
		await questionsApi.answer(questionId, {
			answer: response,
			answerer_type: 'human'
		});

		// SSE will update state automatically, but update optimistically
		this.updateQuestion(questionId, {
			status: 'answered',
			answer: response,
			answerer_type: 'human',
			answered_at: new Date().toISOString()
		});
	}

	/**
	 * Get a single question's full details by ID.
	 */
	async getQuestion(id: string): Promise<Question | null> {
		try {
			return await questionsApi.get(id);
		} catch {
			return null;
		}
	}
}

export const questionsStore = new QuestionsStore();
