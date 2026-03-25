import type { PlanWithStatus } from '$lib/types/plan';

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
		endpoint: '/plan-manager/plans',
		method: 'POST'
	},
	execute: {
		label: 'Execute',
		hint: 'Plan is ready to execute',
		endpoint: '/plan-manager/plans/{slug}/execute',
		method: 'POST'
	}
};

/**
 * Determine chat mode from route and plan state.
 * Accepts an optional plan object instead of reading from a global store.
 */
export function getChatMode(pathname: string, planSlug?: string, plan?: PlanWithStatus | null): ChatMode {
	if (pathname === '/plans') {
		return 'plan';
	}

	if (pathname.startsWith('/plans/') && planSlug) {
		if (plan?.approved) {
			return 'execute';
		}
		return 'chat';
	}

	return 'chat';
}

/**
 * Get full config for a mode.
 */
export function getChatModeConfig(pathname: string, planSlug?: string, plan?: PlanWithStatus | null): ChatModeConfig {
	const mode = getChatMode(pathname, planSlug, plan);
	return { mode, ...MODE_CONFIGS[mode] };
}
