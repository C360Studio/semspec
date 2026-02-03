import { api } from '$lib/api/client';
import type { Message } from '$lib/types';

class MessagesStore {
	messages = $state<Message[]>([]);
	sending = $state(false);

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

			// Add assistant response
			const assistantMessage: Message = {
				id: response.response_id,
				type: messageType as Message['type'],
				content: response.content,
				timestamp: response.timestamp,
				loopId: response.in_reply_to
			};

			this.messages = [...this.messages, assistantMessage];
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
