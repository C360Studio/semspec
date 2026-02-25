import { page } from '$app/stores';
import { get } from 'svelte/store';
import { plansStore } from './plans.svelte';

/**
 * Chat modes determine how user input is routed.
 */
export type ChatMode = 'chat' | 'plan' | 'execute';

export interface ChatModeConfig {
	mode: ChatMode;
	label: string;
	hint: string;
	endpoint: string;
	method: 'POST';
}

/**
 * Mode configurations with routing info.
 */
const MODE_CONFIGS: Record<ChatMode, Omit<ChatModeConfig, 'mode'>> = {
	chat: {
		label: 'Chat',
		hint: 'Ask a question or describe what you need...',
		endpoint: '/agentic-dispatch/message',
		method: 'POST'
	},
	plan: {
		label: 'Planning',
		hint: 'Describe what you want to build...',
		endpoint: '/workflow-api/plans',
		method: 'POST'
	},
	execute: {
		label: 'Execute',
		hint: 'Plan is ready to execute',
		endpoint: '/workflow-api/plans/{slug}/execute',
		method: 'POST'
	}
};

/**
 * Chat mode store - determines mode from current page context.
 */
class ChatModeStore {
	/**
	 * Get current mode based on route and plan state.
	 */
	getMode(pathname: string, planSlug?: string): ChatMode {
		// On plans list page -> Plan mode
		if (pathname === '/plans') {
			return 'plan';
		}

		// On plan detail page -> depends on plan state
		if (pathname.startsWith('/plans/') && planSlug) {
			const plan = plansStore.getBySlug(planSlug);
			if (plan?.approved) {
				return 'execute';
			}
			// Draft plan -> chat about it
			return 'chat';
		}

		// Default -> Chat mode
		return 'chat';
	}

	/**
	 * Get full config for a mode.
	 */
	getConfig(mode: ChatMode): ChatModeConfig {
		return {
			mode,
			...MODE_CONFIGS[mode]
		};
	}

	/**
	 * Get config for current context.
	 */
	getConfigForContext(pathname: string, planSlug?: string): ChatModeConfig {
		const mode = this.getMode(pathname, planSlug);
		return this.getConfig(mode);
	}
}

export const chatModeStore = new ChatModeStore();

/**
 * Helper to get current mode reactively in components.
 * Usage: const mode = $derived(getChatMode($page.url.pathname, planSlug));
 */
export function getChatMode(pathname: string, planSlug?: string): ChatMode {
	return chatModeStore.getMode(pathname, planSlug);
}

/**
 * Helper to get current config reactively in components.
 * Usage: const config = $derived(getChatModeConfig($page.url.pathname, planSlug));
 */
export function getChatModeConfig(pathname: string, planSlug?: string): ChatModeConfig {
	return chatModeStore.getConfigForContext(pathname, planSlug);
}
