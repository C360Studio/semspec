/**
 * ChatDrawer Store - Manages global chat drawer state.
 *
 * The drawer can be scoped to different contexts:
 * - global: General chat accessible from anywhere
 * - plan: Chat scoped to a specific plan
 * - task: Chat scoped to a specific task
 * - question: Chat scoped to a specific question
 */

export interface ChatDrawerContext {
	type: 'global' | 'plan' | 'task' | 'question';
	planSlug?: string;
	taskId?: string;
	questionId?: string;
}

class ChatDrawerStore {
	isOpen = $state(false);
	context = $state<ChatDrawerContext>({ type: 'global' });

	/**
	 * Open the drawer with the specified context.
	 */
	open(context: ChatDrawerContext): void {
		this.context = context;
		this.isOpen = true;
	}

	/**
	 * Close the drawer.
	 */
	close(): void {
		this.isOpen = false;
	}

	/**
	 * Toggle the drawer open/closed.
	 * If context is provided and drawer is closed, opens with that context.
	 * If context is not provided and drawer is closed, opens with current context.
	 */
	toggle(context?: ChatDrawerContext): void {
		if (this.isOpen) {
			this.close();
		} else {
			this.open(context ?? this.context);
		}
	}

	/**
	 * Get the context display title.
	 */
	get contextTitle(): string {
		switch (this.context.type) {
			case 'plan':
				return `Chat - Plan: ${this.context.planSlug}`;
			case 'task':
				return `Chat - Task: ${this.context.taskId}`;
			case 'question':
				return `Chat - Question: ${this.context.questionId}`;
			default:
				return 'Chat';
		}
	}
}

export const chatDrawerStore = new ChatDrawerStore();
