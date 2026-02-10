import { api } from '$lib/api/client';
import type { Message, ActivityEvent } from '$lib/types';

class MessagesStore {
	messages = $state<Message[]>([]);
	sending = $state(false);

	// Track loops we're waiting for responses from
	private pendingLoops = new Set<string>();

	// Handle activity events from the SSE stream
	handleActivityEvent(event: ActivityEvent): void {
		console.log('[messages] handleActivityEvent:', event.type, event);

		// Only handle loop completion events
		if (event.type !== 'loop_updated') return;

		const data = event.data as {
			loop_id?: string;
			outcome?: string;
			result?: string;
		};

		console.log('[messages] loop_updated data:', data);
		console.log('[messages] pendingLoops:', [...this.pendingLoops]);

		// Check if this is a completion with a result
		if (!data?.loop_id || !data?.result) {
			console.log('[messages] skipping - no loop_id or result');
			return;
		}
		if (data.outcome !== 'success') {
			console.log('[messages] skipping - outcome not success:', data.outcome);
			return;
		}

		// Check if we're waiting for this loop
		if (!this.pendingLoops.has(data.loop_id)) {
			console.log('[messages] skipping - loop not in pendingLoops:', data.loop_id);
			return;
		}

		// Remove from pending
		this.pendingLoops.delete(data.loop_id);

		// Add the LLM response as a message
		const responseMessage: Message = {
			id: crypto.randomUUID(),
			type: 'assistant',
			content: data.result,
			timestamp: new Date().toISOString(),
			loopId: data.loop_id
		};

		this.messages = [...this.messages, responseMessage];
	}

	async send(content: string): Promise<void> {
		if (!content.trim() || this.sending) return;

		// Add user message immediately
		const userMessage: Message = {
			id: crypto.randomUUID(),
			type: 'user',
			content,
			timestamp: new Date().toISOString()
		};

		this.messages = [...this.messages, userMessage];
		this.sending = true;

		try {
			const response = await api.router.sendMessage(content);

			// Handle error response from backend
			if (response.error) {
				const errorMessage: Message = {
					id: response.response_id,
					type: 'error',
					content: response.error,
					timestamp: response.timestamp
				};
				this.messages = [...this.messages, errorMessage];
				return;
			}

			// Map backend response type to UI message type
			const messageType = response.type === 'command_response' ? 'status' : 'assistant';

			// Add status response (e.g., "Task submitted. Loop: loop_xxx")
			const statusMessage: Message = {
				id: response.response_id,
				type: messageType as Message['type'],
				content: response.content,
				timestamp: response.timestamp,
				loopId: response.in_reply_to
			};

			this.messages = [...this.messages, statusMessage];

			// Track the loop for async response
			if (response.in_reply_to) {
				this.pendingLoops.add(response.in_reply_to);
			}
		} catch (err) {
			// Add error message
			const errorMessage: Message = {
				id: crypto.randomUUID(),
				type: 'error',
				content: err instanceof Error ? err.message : 'Failed to send message',
				timestamp: new Date().toISOString()
			};

			this.messages = [...this.messages, errorMessage];
		} finally {
			this.sending = false;
		}
	}

	clear(): void {
		this.messages = [];
	}
}

export const messagesStore = new MessagesStore();
